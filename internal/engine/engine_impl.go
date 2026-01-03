package engine

import (
	"fmt"
	"io"
	"os"
	"sync"

	"stackit.dev/stackit/internal/git"
)

// engineImpl is a minimal implementation of the Engine interface
type engineImpl struct {
	repoRoot          string
	trunk             string
	currentBranch     string
	branches          []string
	parentMap         map[string]string    // branch -> parent
	childrenMap       map[string][]string  // branch -> children
	scopeMap          map[string]string    // branch -> scope
	lockedMap         map[string]string    // branch -> lock reason (empty if not locked)
	frozenMap         map[string]bool      // branch -> frozen (local-only)
	remoteShas        map[string]string    // branch -> remote SHA (populated by PopulateRemoteShas)
	remoteMetaCache   map[string]*git.Meta // branch -> remote metadata
	localModified     map[string]bool      // branches with local changes not yet pushed
	maxUndoStackDepth int
	git               git.Runner
	writer            io.Writer
	mu                sync.RWMutex
}

// NewEngine creates a new engine instance
func NewEngine(opts Options) (Engine, error) {
	g := opts.Git
	if g == nil {
		g = git.NewRunnerWithPath(opts.RepoRoot)
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
		parentMap:         make(map[string]string),
		childrenMap:       make(map[string][]string),
		scopeMap:          make(map[string]string),
		lockedMap:         make(map[string]string),
		frozenMap:         make(map[string]bool),
		remoteShas:        make(map[string]string),
		remoteMetaCache:   make(map[string]*git.Meta),
		localModified:     make(map[string]bool),
		maxUndoStackDepth: maxDepth,
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
	if err := e.git.FetchMetadataRefs(); err != nil {
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
	remoteShas, err := e.git.FetchRemoteShas(remote)

	e.mu.Lock()
	defer e.mu.Unlock()

	if err != nil {
		// Don't fail if we can't fetch remote SHAs (e.g., offline)
		// But ensure the map is at least initialized
		if e.remoteShas == nil {
			e.remoteShas = make(map[string]string)
		}
		return nil
	}

	e.remoteShas = remoteShas
	return nil
}
