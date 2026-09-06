// Package git provides low-level Git operations, including repository access,
// branch operations, commit information, PR operations, and metadata management.
package git

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// GetCurrentDate returns the current date and time in yyyyMMddHHmmss format in UTC
func GetCurrentDate() string {
	now := time.Now().UTC()
	return now.Format("20060102150405")
}

// ConfigStore provides typed access to git config. Implemented via direct
// `git config` invocations rather than parsing the config file in process.
type ConfigStore struct {
	repoRoot string

	// stackitKeys is a one-shot snapshot of every stackit.* key, read with a
	// single `git config --get-regexp`. A command reads half a dozen of these
	// (trunk, undo.depth, stack.shape, maxConcurrency, branch.pattern, ...)
	// and each one used to cost its own git process.
	mu            sync.Mutex
	stackitKeys   map[string]string
	snapshotGen   uint64
	snapshotStamp string
}

// configGen is bumped by every write through any ConfigStore, invalidating
// snapshots taken before it. Reads are otherwise served from a snapshot for
// the lifetime of one command, which is the same freshness a single `git
// config --get` per key gave.
var configGen atomic.Uint64

// invalidateSnapshots stales every stackit.* snapshot after a write. Only
// stackit.* writes matter — nothing else is in the snapshot.
func invalidateSnapshots(key string) {
	if snapshotServes(key) {
		configGen.Add(1)
	}
}

// sharedSnapshot is one repository's stackit.* snapshot, reusable by every
// ConfigStore pointing at that repository. Stores are constructed constantly —
// one per config load, per action, per runner config accessor — and each fresh
// one re-ran `git config --get-regexp`, so a single repository's config was
// re-read thousands of times per command batch (2,364 git processes in one
// integration-tier run). The map is never mutated after publication.
type sharedSnapshot struct {
	keys  map[string]string
	gen   uint64
	stamp string
}

// sharedSnapshots maps a repository root to its sharedSnapshot.
var sharedSnapshots sync.Map

// configFiles lists the config files that feed the stackit.* snapshot, in
// git's own precedence order. The snapshot comes from `git config
// --get-regexp` with no scope flag, so it merges global and local — stamping
// only the local file would let a shared snapshot serve a stale global value
// for the life of the process, which matters in the long-lived server where
// a ConfigStore is built per call.
//
// System config (/etc/gitconfig) is deliberately not stamped: it is
// root-owned and effectively static for a process's lifetime.
func configFiles(repoRoot string) []string {
	paths := []string{filepath.Join(GetGitCommonDir(repoRoot), "config")}

	// GIT_CONFIG_GLOBAL replaces the default global paths rather than adding
	// to them, so honor it to the exclusion of the rest. The test helpers
	// set it to /dev/null, which stats cleanly and never changes.
	if p := os.Getenv("GIT_CONFIG_GLOBAL"); p != "" {
		return append(paths, p)
	}
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		paths = append(paths, filepath.Join(xdg, "git", "config"))
	} else if home, err := os.UserHomeDir(); err == nil {
		paths = append(paths, filepath.Join(home, ".config", "git", "config"))
	}
	if home, err := os.UserHomeDir(); err == nil {
		paths = append(paths, filepath.Join(home, ".gitconfig"))
	}
	return paths
}

// configStamp identifies the config files feeding the snapshot by size and
// modification time.
//
// configGen covers writes made through a ConfigStore, but these files are also
// written out of band — `git config` invoked directly, a hook, the user's
// editor, a test helper — and those must invalidate a snapshot shared beyond
// the writing store. Returns "" when a file exists but cannot be stat'ed,
// which disables sharing for that repository and leaves the per-store snapshot
// as the only cache.
func configStamp(repoRoot string) string {
	if repoRoot == "" {
		return ""
	}

	var b strings.Builder
	for _, path := range configFiles(repoRoot) {
		fi, err := os.Stat(path)
		if err != nil {
			if os.IsNotExist(err) {
				// Absent is itself a stampable state: the file contributes
				// nothing now, and creating it later changes the stamp.
				b.WriteString("-;")
				continue
			}
			return ""
		}
		b.WriteString(strconv.FormatInt(fi.Size(), 10))
		b.WriteByte(':')
		b.WriteString(strconv.FormatInt(fi.ModTime().UnixNano(), 10))
		b.WriteByte(';')
	}
	return b.String()
}

// NewConfigStore creates a new ConfigStore for the given repository root.
func NewConfigStore(repoRoot string) *ConfigStore {
	return &ConfigStore{repoRoot: repoRoot}
}

// snapshotServes reports whether key can be answered from the stackit.*
// snapshot.
//
// Restricted to stackit.* on purpose. Keys like branch.<name>.remote carry a
// branch name as their subsection, and a branch name's case is significant —
// serving those from a normalized map would conflate two different branches.
func snapshotServes(key string) bool {
	return strings.HasPrefix(key, "stackit.")
}

// normalizeConfigKey renders a key the way `git config --get-regexp` reports
// it: git lowercases the section and the trailing key name, but preserves the
// subsection in between verbatim (stackit.worktree.basePath is reported as
// stackit.worktree.basepath, while branch.Feature-A.remote keeps Feature-A).
func normalizeConfigKey(key string) string {
	first := strings.Index(key, ".")
	last := strings.LastIndex(key, ".")
	if first < 0 {
		return strings.ToLower(key)
	}
	section := strings.ToLower(key[:first])
	name := strings.ToLower(key[last+1:])
	if first == last {
		return section + "." + name
	}
	return section + key[first:last+1] + name
}

// stackitSnapshot returns the current stackit.* key/value snapshot, reading it
// with a single git process if it is missing or stale.
func (c *ConfigStore) stackitSnapshot() (map[string]string, error) {
	gen := configGen.Load()
	stamp := configStamp(c.repoRoot)

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.stackitKeys != nil && c.snapshotGen == gen && c.snapshotStamp == stamp {
		return c.stackitKeys, nil
	}
	if stamp != "" {
		if v, ok := sharedSnapshots.Load(c.repoRoot); ok {
			if shared, ok := v.(sharedSnapshot); ok && shared.gen == gen && shared.stamp == stamp {
				c.stackitKeys = shared.keys
				c.snapshotGen = gen
				c.snapshotStamp = stamp
				return shared.keys, nil
			}
		}
	}

	// -z is what makes this parseable: it frames each entry as
	// "key\nvalue\0", so a value containing a newline (a multi-line
	// stackit.ci.command, say) stays one record. Without it git separates
	// entries by newline too, and a multi-line value is indistinguishable
	// from the start of the next key.
	out, err := c.runGitConfig("-z", "--get-regexp", `^stackit\.`)
	// Exit code 1 means no stackit.* key is set at all — an empty snapshot,
	// not a failure.
	if err != nil && !isExitCode(err, 1) {
		return nil, fmt.Errorf("failed to list stackit config: %w", err)
	}

	keys := make(map[string]string)
	for _, record := range strings.Split(out, "\x00") {
		if record == "" {
			continue
		}
		// A valueless key ("[stackit]\n\tfoo") has no newline at all; it
		// reads as an empty value, which is what Get returned for it before.
		name, value, _ := strings.Cut(record, "\n")
		// Later records win, matching `git config --get` precedence
		// (system, then global, then local).
		keys[name] = value
	}

	c.stackitKeys = keys
	c.snapshotGen = gen
	c.snapshotStamp = stamp
	if stamp != "" {
		sharedSnapshots.Store(c.repoRoot, sharedSnapshot{keys: keys, gen: gen, stamp: stamp})
	}
	return keys, nil
}

// runGitConfig invokes `git config` in the configured repo. Returns the
// trimmed stdout on success; on non-zero exit returns the trimmed stdout
// (which may be empty) and the *exec.ExitError so callers can match exit
// codes for "key not found" (1) and "nothing to unset" (5).
func (c *ConfigStore) runGitConfig(args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"config"}, args...)...)
	cmd.Dir = c.repoRoot
	out, err := cmd.Output()
	return strings.TrimRight(string(out), "\n"), err
}

func isExitCode(err error, codes ...int) bool {
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return false
	}
	return slices.Contains(codes, exitErr.ExitCode())
}

// Get retrieves a single config value, reading git's merged config
// (system → global → local) — the same precedence `git config --get`
// applies when invoked from inside the repo. Returns empty string if the
// key isn't set in any scope. Use this for keys whose canonical home is
// often the user's global config (e.g. user.name, user.email).
func (c *ConfigStore) Get(key string) (string, error) {
	if snapshotServes(key) {
		keys, err := c.stackitSnapshot()
		if err != nil {
			return "", err
		}
		return keys[normalizeConfigKey(key)], nil
	}

	out, err := c.runGitConfig("--get", key)
	if err == nil {
		return out, nil
	}
	// Exit code 1: key not found. Treat as empty, no error.
	if isExitCode(err, 1) {
		return "", nil
	}
	return "", fmt.Errorf("failed to get config %s: %w", key, err)
}

// GetAll retrieves all values for a multi-value config key.
// Returns empty slice if the key doesn't exist.
func (c *ConfigStore) GetAll(key string) ([]string, error) {
	// -z NUL-terminates each value instead of newline-separating them, so a
	// value containing an embedded newline (a multi-line approved hook
	// command, say) stays one entry instead of splitting into extras that
	// never match the original value again.
	out, err := c.runGitConfig("-z", "--get-all", key)
	if err != nil {
		if isExitCode(err, 1) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get config %s: %w", key, err)
	}
	if out == "" {
		return nil, nil
	}
	// -z terminates every value, leaving one trailing empty element after
	// Split; drop it rather than filtering all empties, which would also
	// discard a legitimate empty-string value.
	values := strings.Split(out, "\x00")
	return values[:len(values)-1], nil
}

// Set sets a config value in local git config.
func (c *ConfigStore) Set(key, value string) error {
	defer invalidateSnapshots(key)
	if _, err := c.runGitConfig(key, value); err != nil {
		return fmt.Errorf("failed to set config %s: %w", key, err)
	}
	return nil
}

// SetBool sets a boolean config value.
func (c *ConfigStore) SetBool(key string, value bool) error {
	return c.Set(key, strconv.FormatBool(value))
}

// SetInt sets an integer config value.
func (c *ConfigStore) SetInt(key string, value int) error {
	return c.Set(key, strconv.Itoa(value))
}

// Add adds a value to a multi-value config key.
func (c *ConfigStore) Add(key, value string) error {
	defer invalidateSnapshots(key)
	if _, err := c.runGitConfig("--add", key, value); err != nil {
		return fmt.Errorf("failed to add config %s: %w", key, err)
	}
	return nil
}

// Unset removes all values for a config key.
// Does not return an error if the key doesn't exist.
func (c *ConfigStore) Unset(key string) error {
	defer invalidateSnapshots(key)
	_, err := c.runGitConfig("--unset-all", key)
	if err == nil {
		return nil
	}
	// Exit code 5: no such section/key. Treat as success (idempotent unset).
	if isExitCode(err, 5) {
		return nil
	}
	return fmt.Errorf("failed to unset config %s: %w", key, err)
}

// GetBool retrieves a boolean config value.
// Returns false and no error if the key doesn't exist.
func (c *ConfigStore) GetBool(key string) (bool, error) {
	val, err := c.Get(key)
	if err != nil || val == "" {
		return false, err
	}
	return strconv.ParseBool(val)
}

// GetBoolWithDefault retrieves a boolean config value with a default.
func (c *ConfigStore) GetBoolWithDefault(key string, defaultValue bool) bool {
	raw, _ := c.Get(key)
	if raw == "" {
		return defaultValue
	}
	val, err := strconv.ParseBool(raw)
	if err != nil {
		return defaultValue
	}
	return val
}

// GetInt retrieves an integer config value.
// Returns 0 and no error if the key doesn't exist.
func (c *ConfigStore) GetInt(key string) (int, error) {
	val, err := c.Get(key)
	if err != nil || val == "" {
		return 0, err
	}
	return strconv.Atoi(val)
}

// GetIntWithDefault retrieves an integer config value with a default.
func (c *ConfigStore) GetIntWithDefault(key string, defaultValue int) int {
	raw, _ := c.Get(key)
	if raw == "" {
		return defaultValue
	}
	val, err := strconv.Atoi(raw)
	if err != nil {
		return defaultValue
	}
	return val
}

// Exists checks if a config key exists.
func (c *ConfigStore) Exists(key string) bool {
	val, err := c.Get(key)
	return err == nil && val != ""
}
