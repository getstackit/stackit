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

// EntryConfig is the input to NewEntry. It captures the per-repo state the
// constructor needs to wire up the broadcaster + watcher.
type EntryConfig struct {
	ID          string
	DisplayName string
	RepoRoot    string
	Remote      string
	Engine      engine.Engine
	GitHub      github.Client
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
	Engine      engine.Engine
	GitHub      github.Client

	Broadcaster *Broadcaster
	Watcher     *watcher.RefWatcher

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
		Engine:      cfg.Engine,
		GitHub:      cfg.GitHub,
		Broadcaster: NewBroadcaster(),
	}
	entry.AddCloser(func() error { entry.Broadcaster.Close(); return nil })

	if cfg.RepoRoot != "" && cfg.Engine != nil {
		entry.Watcher = watcher.NewRefWatcher(cfg.RepoRoot, watcherDebounce, makeWatcherCallback(entry))
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

// makeWatcherCallback returns the function the ref watcher invokes when
// the repo's refs change. It rebuilds the engine, detects branch switches,
// and re-broadcasts both "branch_switched" and "refresh" so connected SSE
// clients refetch.
func makeWatcherCallback(entry *RepoEntry) func() {
	trunkName := entry.Engine.Trunk().GetName()
	var lastCurrentBranch string
	if cb := entry.Engine.CurrentBranch(); cb != nil {
		lastCurrentBranch = cb.GetName()
	}

	return func() {
		if err := entry.Engine.Rebuild(trunkName); err != nil {
			slog.Error("engine rebuild failed", "repo", entry.ID, "error", err)
			return
		}

		if cb := entry.Engine.CurrentBranch(); cb != nil {
			newBranch := cb.GetName()
			if lastCurrentBranch != "" && newBranch != lastCurrentBranch {
				data, _ := json.Marshal(map[string]string{
					"from":      lastCurrentBranch,
					"to":        newBranch,
					"timestamp": time.Now().Format(time.RFC3339),
				})
				entry.Broadcaster.Broadcast("branch_switched", string(data))
			}
			lastCurrentBranch = newBranch
		}

		entry.Broadcaster.Broadcast("refresh", "{}")
	}
}

// Registry is a goroutine-safe collection of RepoEntries keyed by ID.
type Registry struct {
	mu      sync.RWMutex
	entries map[string]*RepoEntry
}

// New returns an empty registry.
func New() *Registry {
	return &Registry{entries: make(map[string]*RepoEntry)}
}

// Add inserts an entry. The entry's ID must be non-empty, match the allowed
// pattern, and be unique within the registry.
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
	r.entries[e.ID] = e
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
	return errors.Join(errs...)
}
