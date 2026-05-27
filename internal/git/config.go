// Package git provides low-level Git operations, including repository access,
// branch operations, commit information, PR operations, and metadata management.
package git

import (
	"errors"
	"fmt"
	"os/exec"
	"slices"
	"strconv"
	"strings"
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
}

// NewConfigStore creates a new ConfigStore for the given repository root.
func NewConfigStore(repoRoot string) *ConfigStore {
	return &ConfigStore{repoRoot: repoRoot}
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

// Get retrieves a single config value from local git config.
// Returns empty string if the key doesn't exist.
func (c *ConfigStore) Get(key string) (string, error) {
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
	out, err := c.runGitConfig("--get-all", key)
	if err != nil {
		if isExitCode(err, 1) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get config %s: %w", key, err)
	}
	if out == "" {
		return nil, nil
	}
	values := strings.Split(out, "\n")
	return values, nil
}

// Set sets a config value in local git config.
func (c *ConfigStore) Set(key, value string) error {
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
	if _, err := c.runGitConfig("--add", key, value); err != nil {
		return fmt.Errorf("failed to add config %s: %w", key, err)
	}
	return nil
}

// Unset removes all values for a config key.
// Does not return an error if the key doesn't exist.
func (c *ConfigStore) Unset(key string) error {
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
