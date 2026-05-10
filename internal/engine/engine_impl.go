package engine

import (
	"context"
	"fmt"
	"io"
	"maps"
	"os"
	"slices"
	"sync"

	"github.com/getstackit/stackit/internal/git"
)

// engineImpl is a minimal implementation of the Engine interface
type engineImpl struct {
	repoRoot          string
	trunk             string
	currentBranch     string
	state             *stateCore
	remoteMetaCache   map[string]*git.Meta // branch -> remote metadata (can include non-local branches)
	maxUndoStackDepth int
	maxConcurrency    int
	git               git.Runner
	writer            io.Writer
	mu                sync.RWMutex
	worktreeMu        sync.Mutex // serializes worktree add/remove/prune to avoid git races on .git/worktrees/

	// Temporary worktree cleanup tracking.
	tempWorktreeNeedsPrune bool
	tempWorktreePrunedOnce bool
}

// WorktreeSnapshot holds a deep copy of engine state for initializing worktree engines.
// This avoids the cost of rebuildInternal (which reads all branches + metadata from git)
// since worktrees share .git with the parent and the data is identical.
type WorktreeSnapshot struct {
	Trunk           string
	Branches        []string
	BranchState     BranchStateMap
	ChildrenMap     map[string][]string
	RemoteMetaCache map[string]*git.Meta
	MaxConcurrency  int
}

// SnapshotForWorktree creates a deep copy of the engine's mutable state for use in
// worktree sessions. The snapshot is safe to use from another goroutine since all
// maps and slices are deep-copied.
func (e *engineImpl) SnapshotForWorktree() WorktreeSnapshot {
	e.mu.RLock()
	defer e.mu.RUnlock()

	// Deep copy branches slice
	branches := make([]string, len(e.state.branches))
	copy(branches, e.state.branches)

	// Deep copy branchState map
	branchState := make(BranchStateMap, len(e.state.branchState))
	for name, state := range e.state.branchState {
		copied := *state
		branchState[name] = &copied
	}

	// Deep copy childrenMap
	childrenMap := make(map[string][]string, len(e.state.childrenMap))
	for parent, children := range e.state.childrenMap {
		childrenCopy := make([]string, len(children))
		copy(childrenCopy, children)
		childrenMap[parent] = childrenCopy
	}

	// Shallow copy remoteMetaCache — Meta values are treated as immutable
	remoteMetaCache := make(map[string]*git.Meta, len(e.remoteMetaCache))
	maps.Copy(remoteMetaCache, e.remoteMetaCache)

	return WorktreeSnapshot{
		Trunk:           e.trunk,
		Branches:        branches,
		BranchState:     branchState,
		ChildrenMap:     childrenMap,
		RemoteMetaCache: remoteMetaCache,
		MaxConcurrency:  e.maxConcurrency,
	}
}

// WorktreeEngineOptions configures NewEngineForWorktree.
type WorktreeEngineOptions struct {
	// WorktreePath is the root directory of the worktree.
	WorktreePath string

	// Snapshot is the parent engine's state snapshot.
	Snapshot WorktreeSnapshot

	// Writer is the output writer for warnings. If nil, os.Stderr is used.
	Writer io.Writer
}

// NewEngineForWorktree creates an engine for a worktree session using a snapshot
// from the parent engine. This skips rebuildInternal and maybeAutoFetchRemoteMetadata
// since worktrees share .git with the parent and the metadata is identical.
func NewEngineForWorktree(opts WorktreeEngineOptions) (Engine, error) {
	g := git.NewRunnerWithPath(opts.WorktreePath, nil)

	if err := g.InitDefaultRepo(); err != nil {
		return nil, fmt.Errorf("failed to initialize worktree git repository: %w", err)
	}

	writer := opts.Writer
	if writer == nil {
		writer = os.Stderr
	}

	// Sort children for deterministic traversal (snapshot should already be sorted,
	// but ensure consistency)
	for _, children := range opts.Snapshot.ChildrenMap {
		slices.Sort(children)
	}

	e := &engineImpl{
		repoRoot:          opts.WorktreePath,
		trunk:             opts.Snapshot.Trunk,
		state:             newStateCoreFromSnapshot(opts.Snapshot.Branches, opts.Snapshot.BranchState, opts.Snapshot.ChildrenMap),
		remoteMetaCache:   opts.Snapshot.RemoteMetaCache,
		maxUndoStackDepth: 0, // No undo in temporary worktrees
		maxConcurrency:    opts.Snapshot.MaxConcurrency,
		git:               g,
		writer:            writer,
	}

	// Get current branch (1 cheap git call — needed for worktree's HEAD)
	currentBranch, err := g.GetCurrentBranch()
	if err != nil {
		currentBranch = ""
	}
	e.currentBranch = currentBranch

	return e, nil
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
		state:             newStateCore(),
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
		if slices.Contains(refspecs, metadataRefspec) {
			// Already configured, nothing to do - this is the fast path
			// Skip loading remote metadata cache here - it's not needed for most commands
			// and will be loaded lazily when actually needed (e.g., during sync operations)
			return
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

	if len(metadataRefs) > 0 {
		refNames := make([]string, 0, len(metadataRefs))
		for branchName := range metadataRefs {
			refNames = append(refNames, git.MetadataRefName(branchName))
		}
		if err := e.git.DeleteRefsBatch(context.Background(), refNames); err != nil {
			return fmt.Errorf("failed to delete metadata refs: %w", err)
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
	for _, state := range e.state.branchState {
		state.RemoteSHA = ""
	}

	if err != nil {
		// Don't fail if we can't fetch remote SHAs (e.g., offline)
		return nil
	}

	// Set RemoteSHA for tracked branches that have a remote
	for branchName, sha := range remoteShas {
		if state := e.state.branchState.GetByName(branchName); state != nil {
			state.RemoteSHA = sha
		}
	}
	return nil
}
