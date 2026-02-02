package engine

import (
	"context"
	"fmt"
	"io"
	"os"
	"sync"

	"stackit.dev/stackit/internal/git"
)

// BranchState holds the cached metadata state for a single branch.
// This consolidates what was previously stored in separate maps.
type BranchState struct {
	Parent        string         // Parent branch name
	Scope         string         // Scope string (may be empty)
	LockReason    git.LockReason // Lock reason (empty if not locked)
	Frozen        bool           // Whether branch is frozen (local-only state)
	BranchType    git.BranchType // Branch type (worktree-anchor, utility, etc.)
	RemoteSHA     string         // Remote SHA (populated by PopulateRemoteShas)
	LocalModified bool           // Has local metadata changes not yet pushed
}

// HasScope returns true if this branch has an explicit scope set.
func (s *BranchState) HasScope() bool {
	return s.Scope != ""
}

// GetScope returns the scope as a Scope type.
func (s *BranchState) GetScope() Scope {
	return NewScope(s.Scope)
}

// IsLocked returns true if this branch is locked.
func (s *BranchState) IsLocked() bool {
	return s.LockReason.IsLocked()
}

// BranchStateMap is a map of branch names to their state.
type BranchStateMap map[string]*BranchState

// Get returns the state for a branch, or nil if not found.
func (m BranchStateMap) Get(branch Branch) *BranchState {
	return m[branch.GetName()]
}

// GetByName returns the state for a branch name, or nil if not found.
func (m BranchStateMap) GetByName(name string) *BranchState {
	return m[name]
}

// Has returns true if the branch exists in the map.
func (m BranchStateMap) Has(branch Branch) bool {
	_, ok := m[branch.GetName()]
	return ok
}

// HasByName returns true if the branch name exists in the map.
func (m BranchStateMap) HasByName(name string) bool {
	_, ok := m[name]
	return ok
}

// Set sets the state for a branch.
func (m BranchStateMap) Set(name string, state *BranchState) {
	m[name] = state
}

// Delete removes a branch from the map.
func (m BranchStateMap) Delete(name string) {
	delete(m, name)
}

// GetOrCreate returns the state for a branch, creating it if it doesn't exist.
func (m BranchStateMap) GetOrCreate(name string) *BranchState {
	if state, ok := m[name]; ok {
		return state
	}
	state := &BranchState{}
	m[name] = state
	return state
}

// engineImpl is a minimal implementation of the Engine interface
type engineImpl struct {
	repoRoot          string
	trunk             string
	currentBranch     string
	branches          []string
	branchNamesSet    *BranchSet           // cached set for O(1) lookups, lazily built
	branchState       BranchStateMap       // branch -> consolidated state
	childrenMap       map[string][]string  // branch -> children (computed from parents)
	remoteMetaCache   map[string]*git.Meta // branch -> remote metadata (can include non-local branches)
	maxUndoStackDepth int
	maxConcurrency    int
	git               git.Runner
	writer            io.Writer
	mu                sync.RWMutex
	worktreeMu        sync.Mutex // serializes worktree creation to avoid git races on .git/worktrees/
}

// NewEngine creates a new engine instance
func NewEngine(opts Options) (Engine, error) {
	g := opts.Git
	if g == nil {
		g = git.NewRunnerWithPath(opts.RepoRoot, nil)
	}

	if err := g.InitDefaultRepo(); err != nil {
		return nil, fmt.Errorf("failed to initialize git repository: %w", err)
	}

	if opts.RepoRoot == "" {
		return nil, fmt.Errorf("repo root must be specified in Options")
	}

	if opts.Trunk == "" {
		return nil, fmt.Errorf("trunk must be specified in Options")
	}

	maxDepth := opts.MaxUndoStackDepth
	if maxDepth <= 0 {
		maxDepth = DefaultMaxUndoStackDepth
	}

	writer := opts.Writer
	if writer == nil {
		writer = os.Stderr
	}

	e := &engineImpl{
		repoRoot:          opts.RepoRoot,
		trunk:             opts.Trunk,
		branchState:       make(BranchStateMap),
		childrenMap:       make(map[string][]string),
		remoteMetaCache:   make(map[string]*git.Meta),
		maxUndoStackDepth: maxDepth,
		maxConcurrency:    opts.MaxConcurrency,
		git:               g,
		writer:            writer,
	}

	currentBranch, err := g.GetCurrentBranch()
	if err != nil {
		currentBranch = ""
	}
	e.currentBranch = currentBranch

	// Don't refresh currentBranch here since we just set it
	if err := e.rebuildInternal(false); err != nil {
		return nil, fmt.Errorf("failed to rebuild engine: %w", err)
	}

	// Auto-fetch remote metadata on first use (fresh clone scenario)
	// Skip if refspec is already configured to avoid unnecessary work
	e.maybeAutoFetchRemoteMetadata()

	return e, nil
}

// maybeAutoFetchRemoteMetadata fetches remote metadata if this appears to be a fresh clone
func (e *engineImpl) maybeAutoFetchRemoteMetadata() {
	// Fast path: Check if refspec is already configured (most common case)
	// This avoids expensive git config reads and network fetches for normal operations
	refspecs, err := e.git.GetConfigAll("remote.origin.fetch")
	if err == nil {
		metadataRefspec := "+refs/stackit/metadata/*:refs/stackit/remote-metadata/*"
		for _, rs := range refspecs {
			if rs == metadataRefspec {
				// Already configured, nothing to do - this is the fast path
				// Skip loading remote metadata cache here - it's not needed for most commands
				// and will be loaded lazily when actually needed (e.g., during sync operations)
				return
			}
		}
	}

	// Not configured yet - this might be a fresh clone
	// Try to fetch metadata refs (this is a network operation, so it's slow)
	// Only do this if refspec isn't configured to avoid slowing down every command
	if err := e.git.FetchMetadataRefs(context.Background()); err != nil {
		// No remote metadata available, or error fetching - that's okay
		return
	}

	// Configure refspec for future fetches
	_ = e.git.EnsureMetadataRefspecConfigured()

	// Load remote metadata cache (only if we just fetched)
	_ = e.LoadRemoteMetadataCache()
}

// Reset clears all branch metadata and rebuilds with new trunk
func (e *engineImpl) Reset(newTrunkName string) error {
	metadataRefs, err := e.git.ListMetadata()
	if err != nil {
		return fmt.Errorf("failed to get metadata refs: %w", err)
	}

	for branchName := range metadataRefs {
		if err := e.git.DeleteMetadata(branchName); err != nil {
			continue
		}
	}

	e.mu.Lock()
	e.trunk = newTrunkName
	e.mu.Unlock()

	return e.rebuild()
}

func (e *engineImpl) Git() git.Runner {
	return e.git
}

// Rebuild reloads branch cache with new trunk
func (e *engineImpl) Rebuild(newTrunkName string) error {
	e.mu.Lock()
	e.trunk = newTrunkName
	e.mu.Unlock()

	return e.rebuild()
}

// PopulateRemoteShas populates remote branch information by fetching SHAs from remote
func (e *engineImpl) PopulateRemoteShas() error {
	remote := e.git.GetRemote()
	remoteShas, err := e.git.FetchRemoteShas(context.Background(), remote)

	e.mu.Lock()
	defer e.mu.Unlock()

	// Clear existing remote SHAs
	for _, state := range e.branchState {
		state.RemoteSHA = ""
	}

	if err != nil {
		// Don't fail if we can't fetch remote SHAs (e.g., offline)
		return nil
	}

	// Set RemoteSHA for tracked branches that have a remote
	for branchName, sha := range remoteShas {
		if state := e.branchState.GetByName(branchName); state != nil {
			state.RemoteSHA = sha
		}
	}
	return nil
}
