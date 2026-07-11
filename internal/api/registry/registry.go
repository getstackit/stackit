// Package registry holds the set of repositories served by the stackit
// server, keyed by a stable repoID. Each entry owns the per-repository
// engine, GitHub client, event broadcaster, and ref watcher — so multiple
// repositories can be served from one process without leaking events
// between them.
package registry

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/getstackit/stackit/internal/api/watcher"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/github"
)

// watcherDebounce is the interval the ref watcher coalesces filesystem
// events over before notifying the engine. Matches the previous server-
// scoped value.
const watcherDebounce = 200 * time.Millisecond

// idPattern restricts repo IDs to characters that survive in URL paths
// without escaping, keeping the routing surface simple.
var idPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// RepoRef identifies a GitHub repository by its owner/name coordinates.
type RepoRef struct {
	Owner string
	Name  string
}

// Key returns the canonical lookup key for the repo. GitHub treats owner/repo
// case-insensitively, so we lowercase to match regardless of how the
// coordinates were typed.
func (r RepoRef) Key() string {
	return strings.ToLower(r.Owner + "/" + r.Name)
}

// String returns the "owner/name" form for logs and errors.
func (r RepoRef) String() string {
	return r.Owner + "/" + r.Name
}

// EntryConfig is the input to NewEntry. It captures the per-repo state the
// constructor needs to wire up the broadcaster + watcher.
type EntryConfig struct {
	ID          string
	DisplayName string
	RepoRoot    string
	Remote      string
	// AddedBy is the GitHub login of the user who onboarded this repo, or
	// empty for operator-seeded repos. Handlers use it to scope per-user
	// visibility so a user only sees repos they added.
	AddedBy string
	// Managed marks a server-owned mirror checkout (DB-backed / onboarded under
	// the repos root) that the sync loop may mirror-fetch. The -cwd dev repo is
	// the operator's own working tree and is left unmanaged.
	Managed bool
	// RepoRef carries the GitHub coordinates, set for managed repos. The
	// sync loop uses them to resolve a GitHub App installation token for the
	// fetch.
	RepoRef
	Engine engine.Engine
	GitHub github.Client
}

// RepoEntry is the per-repository state required to serve API requests for
// one repo. Constructed via NewEntry so the watcher + broadcaster lifecycle
// stays consistent across call sites; raw struct literals are still usable
// in tests where neither is needed.
type RepoEntry struct {
	ID          string
	DisplayName string
	RepoRoot    string
	Remote      string
	// AddedBy is the GitHub login of the user who onboarded this repo, or
	// empty for operator-seeded repos visible to everyone.
	AddedBy string
	// Managed marks a server-owned mirror checkout the sync loop may
	// mirror-fetch (see EntryConfig.Managed).
	Managed bool
	// RepoRef carries the GitHub coordinates, set for managed repos; the
	// sync loop resolves an installation token from them.
	RepoRef
	Engine engine.Engine
	GitHub github.Client

	Broadcaster *Broadcaster
	Watcher     *watcher.RefWatcher

	// refresh state: trunkName is captured at construction; lastCurrentBranch
	// tracks the served HEAD across refreshes to detect branch switches.
	// refreshMu serializes refresh() so the ref watcher and the sync loop
	// can't rebuild the engine concurrently.
	refreshMu         sync.Mutex
	trunkName         string
	lastCurrentBranch string

	closers []func() error
}

// NewEntry constructs a ready-to-use entry with a broadcaster, a ref
// watcher (when RepoRoot is set), and closers that cleanly tear down both
// when registry.Close is called. The watcher goroutine starts immediately.
func NewEntry(cfg EntryConfig) *RepoEntry {
	entry := &RepoEntry{
		ID:          cfg.ID,
		DisplayName: cfg.DisplayName,
		RepoRoot:    cfg.RepoRoot,
		Remote:      cfg.Remote,
		AddedBy:     cfg.AddedBy,
		Managed:     cfg.Managed,
		RepoRef:     cfg.RepoRef,
		Engine:      cfg.Engine,
		GitHub:      cfg.GitHub,
		Broadcaster: NewBroadcaster(),
	}
	entry.AddCloser(func() error { entry.Broadcaster.Close(); return nil })

	if cfg.RepoRoot != "" && cfg.Engine != nil {
		entry.trunkName = cfg.Engine.Trunk().GetName()
		if cb := cfg.Engine.CurrentBranch(); cb != nil {
			entry.lastCurrentBranch = cb.GetName()
		}
		entry.Watcher = watcher.NewRefWatcher(cfg.RepoRoot, watcherDebounce, entry.Refresh)
		watcherDone := make(chan error, 1)
		go func() {
			watcherDone <- entry.Watcher.Start()
		}()
		entry.AddCloser(func() error {
			entry.Watcher.Stop()
			if err := <-watcherDone; err != nil {
				return fmt.Errorf("ref watcher: %w", err)
			}
			return nil
		})
	}

	return entry
}

// AddCloser registers a teardown function that Registry.Close will invoke
// in reverse-registration order. Use it to attach lifecycle hooks (logger
// closers, etc.) when constructing an entry.
func (e *RepoEntry) AddCloser(fn func() error) {
	if fn == nil {
		return
	}
	e.closers = append(e.closers, fn)
}

func (e *RepoEntry) close() error {
	var errs []error
	for i := len(e.closers) - 1; i >= 0; i-- {
		if err := e.closers[i](); err != nil {
			errs = append(errs, err)
		}
	}
	e.closers = nil
	return errors.Join(errs...)
}

// Refresh rebuilds the engine from current refs and broadcasts a "refresh"
// event (plus "branch_switched" when the served HEAD changed) so connected SSE
// clients refetch. It is the shared post-change path invoked by both the ref
// watcher (local changes) and the sync loop (remote changes); refreshMu
// serializes the two so the engine isn't rebuilt concurrently.
func (e *RepoEntry) Refresh() {
	if e.Engine == nil {
		return
	}
	e.refreshMu.Lock()
	defer e.refreshMu.Unlock()

	if err := e.Engine.Rebuild(e.trunkName); err != nil {
		slog.Error("engine rebuild failed", "repo", e.ID, "error", err)
		return
	}

	if cb := e.Engine.CurrentBranch(); cb != nil {
		newBranch := cb.GetName()
		if e.lastCurrentBranch != "" && newBranch != e.lastCurrentBranch {
			data, _ := json.Marshal(map[string]string{
				"from":      e.lastCurrentBranch,
				"to":        newBranch,
				"timestamp": time.Now().Format(time.RFC3339),
			})
			e.Broadcaster.Broadcast("branch_switched", string(data))
		}
		e.lastCurrentBranch = newBranch
	}

	e.Broadcaster.Broadcast("refresh", "{}")
}

// Registry is a goroutine-safe collection of RepoEntries keyed by ID, with a
// secondary index keyed by owner/repo so handlers can resolve the GitHub-style
// /repos/{owner}/{repo} routes.
type Registry struct {
	mu      sync.RWMutex
	entries map[string]*RepoEntry
	// byOwnerRepo maps a lowercased "owner/name" to the same entries. Only
	// entries with both an owner and a name are indexed; a repo with no GitHub
	// remote (empty coordinates) is reachable by ID but not by owner/repo.
	byOwnerRepo map[string]*RepoEntry
}

// New returns an empty registry.
func New() *Registry {
	return &Registry{
		entries:     make(map[string]*RepoEntry),
		byOwnerRepo: make(map[string]*RepoEntry),
	}
}

// Add inserts an entry. The entry's ID must be non-empty, match the allowed
// pattern, and be unique within the registry. When the entry carries GitHub
// coordinates (Owner and Name), they must also be unique so the owner/repo
// index stays unambiguous.
func (r *Registry) Add(e *RepoEntry) error {
	if e == nil {
		return errors.New("registry: nil entry")
	}
	if e.ID == "" {
		return errors.New("registry: entry has empty ID")
	}
	if !idPattern.MatchString(e.ID) {
		return fmt.Errorf("registry: invalid repo ID %q (must match %s)", e.ID, idPattern)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.entries[e.ID]; exists {
		return fmt.Errorf("registry: duplicate repo ID %q", e.ID)
	}
	var key string
	if e.Owner != "" && e.Name != "" {
		key = e.Key()
		if _, exists := r.byOwnerRepo[key]; exists {
			return fmt.Errorf("registry: duplicate repo %s", e.RepoRef)
		}
	}
	r.entries[e.ID] = e
	if key != "" {
		r.byOwnerRepo[key] = e
	}
	return nil
}

// Get returns the entry for id. The bool is false when no such entry
// exists, matching the comma-ok idiom of map access.
func (r *Registry) Get(id string) (*RepoEntry, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.entries[id]
	return e, ok
}

// GetByOwnerRepo returns the entry for owner/repo, matched case-insensitively.
// The bool is false when no such entry exists or the coordinates are empty.
func (r *Registry) GetByOwnerRepo(owner, repo string) (*RepoEntry, bool) {
	if owner == "" || repo == "" {
		return nil, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.byOwnerRepo[RepoRef{Owner: owner, Name: repo}.Key()]
	return e, ok
}

// FindManaged returns the managed entry whose GitHub coordinates match repo
// case-insensitively, or false when none does. The interval sync loop and the
// webhook receiver use it to map a remote-change signal (a push for
// owner/name) onto the local checkout to refresh.
//
// Only managed mirrors are considered: the unmanaged -cwd working repo must
// never be selected here, because the sync path mirror-fetches into a detached
// HEAD and would corrupt a real working tree (see safety-invariants.md).
func (r *Registry) FindManaged(repo RepoRef) (*RepoEntry, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	key := repo.Key()
	for _, e := range r.entries {
		if e.Managed && e.Key() == key {
			return e, true
		}
	}
	return nil, false
}

// List returns every entry sorted by ID so callers (and tests) get a stable
// ordering for serialization.
func (r *Registry) List() []*RepoEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*RepoEntry, 0, len(r.entries))
	for _, e := range r.entries {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Len reports the number of registered entries.
func (r *Registry) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.entries)
}

// Close runs every entry's closers and returns the joined error. The
// registry is unusable afterwards.
func (r *Registry) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	var errs []error
	for _, e := range r.entries {
		if err := e.close(); err != nil {
			errs = append(errs, fmt.Errorf("registry: closing %q: %w", e.ID, err))
		}
	}
	r.entries = nil
	r.byOwnerRepo = nil
	return errors.Join(errs...)
}
