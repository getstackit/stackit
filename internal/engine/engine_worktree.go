package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/getstackit/stackit/internal/git"
)

// WorktreeCheckoutMode specifies how files are checked out in a worktree.
type WorktreeCheckoutMode int

const (
	// WorktreeCheckoutFull checks out all files (default behavior).
	WorktreeCheckoutFull WorktreeCheckoutMode = iota
	// WorktreeCheckoutShallow creates a worktree without checking out files.
	// This is faster for validation-only operations that don't need actual files.
	WorktreeCheckoutShallow
)

// WorktreePruneMode specifies whether to prune stale worktrees before creation.
type WorktreePruneMode int

const (
	// WorktreePruneAuto prunes stale worktrees before creating a new one (default).
	WorktreePruneAuto WorktreePruneMode = iota
	// WorktreePruneSkip skips pruning. Use when the caller has already pruned
	// (e.g., parallel worktree creation).
	WorktreePruneSkip
)

// AddWorktree adds a new worktree
func (e *engineImpl) AddWorktree(ctx context.Context, path string, branch string, detach git.WorktreeDetachMode) error {
	return e.git.AddWorktree(ctx, path, branch, detach)
}

// RemoveWorktree removes a worktree
func (e *engineImpl) RemoveWorktree(ctx context.Context, path string) error {
	return e.git.RemoveWorktree(ctx, path)
}

// ForceRemoveWorktree removes a worktree even if it contains uncommitted changes.
func (e *engineImpl) ForceRemoveWorktree(ctx context.Context, path string) error {
	return e.git.ForceRemoveWorktree(ctx, path)
}

// GetWorktreeCurrentBranch returns the branch currently checked out in the
// worktree at worktreePath.
func (e *engineImpl) GetWorktreeCurrentBranch(ctx context.Context, worktreePath string) (string, error) {
	return e.git.GetWorktreeCurrentBranch(ctx, worktreePath)
}

// WorktreeHasUncommittedChanges reports whether the worktree at worktreePath has
// staged, unstaged, or untracked changes.
func (e *engineImpl) WorktreeHasUncommittedChanges(ctx context.Context, worktreePath string) (bool, error) {
	return e.git.WorktreeHasUncommittedChanges(ctx, worktreePath)
}

// WorktreeHasTrackedChanges reports whether the worktree at worktreePath has
// staged or unstaged changes to tracked files, ignoring untracked ones. Use this
// when the question is "would a hard reset destroy something", which untracked
// files never answer yes to.
func (e *engineImpl) WorktreeHasTrackedChanges(ctx context.Context, worktreePath string) (bool, error) {
	return e.git.WorktreeHasTrackedChanges(ctx, worktreePath)
}

// PruneWorktrees removes stale worktree entries from .git/worktrees.
// This cleans up worktree information for worktrees whose working directory
// has been deleted or is otherwise unavailable.
func (e *engineImpl) PruneWorktrees(ctx context.Context) error {
	e.worktreeMu.Lock()
	defer e.worktreeMu.Unlock()

	if err := e.git.PruneWorktrees(ctx); err != nil {
		return err
	}

	e.tempWorktreeNeedsPrune = false
	e.tempWorktreePrunedOnce = true
	return nil
}

// CreateTemporaryWorktree creates a temporary directory and adds a detached worktree
func (e *engineImpl) CreateTemporaryWorktree(ctx context.Context, branch string, prefix string) (string, func(), error) {
	return e.CreateTemporaryWorktreeWithOptions(ctx, branch, prefix, WorktreeCheckoutFull, WorktreePruneAuto)
}

// CreateTemporaryWorktreeSkipPrune is like CreateTemporaryWorktree but skips the automatic
// PruneWorktrees() call. Use this when creating multiple worktrees in parallel after
// manually calling PruneWorktrees() once, to avoid race conditions.
func (e *engineImpl) CreateTemporaryWorktreeSkipPrune(ctx context.Context, branch string, prefix string) (string, func(), error) {
	return e.CreateTemporaryWorktreeWithOptions(ctx, branch, prefix, WorktreeCheckoutFull, WorktreePruneSkip)
}

// CreateTemporaryWorktreeWithOptions creates a temporary directory and adds a detached worktree with options.
//
// checkout controls whether files are checked out:
//   - WorktreeCheckoutFull: checks out all files (default behavior)
//   - WorktreeCheckoutShallow: creates worktree without checking out files (faster for validation)
//
// prune controls whether stale worktrees are pruned before creation:
//   - WorktreePruneAuto: prunes stale worktrees first (default behavior)
//   - WorktreePruneSkip: skips pruning (use when caller has already pruned for parallel creation)
//
// Note: Callers that create multiple worktrees in parallel (like ValidateRebasesParallel) should call
// PruneWorktrees() once before starting parallel worktree creation and pass WorktreePruneSkip to avoid race conditions.
func (e *engineImpl) CreateTemporaryWorktreeWithOptions(ctx context.Context, branch string, prefix string, checkout WorktreeCheckoutMode, prune WorktreePruneMode) (string, func(), error) {
	tmpDir, err := os.MkdirTemp("", prefix)
	if err != nil {
		return "", nil, fmt.Errorf("failed to create temporary directory: %w", err)
	}

	// Use the unique temp directory basename as the worktree name to avoid collisions
	// in Git's .git/worktrees/ registry. Previously using a fixed "worktree" name caused
	// intermittent failures when stale entries remained after incomplete cleanup.
	worktreePath := filepath.Join(tmpDir, filepath.Base(tmpDir))

	// Serialize worktree operations to prevent races on .git/worktrees/ directory.
	// Git's `worktree add` command is not concurrency-safe - when multiple goroutines
	// run it simultaneously on the same repo, they can race on reading/writing the
	// .git/worktrees/ directory, causing "failed to read commondir" errors.
	//
	// The mutex ensures only one worktree is being created at a time per engine (repo).
	// This is acceptable because:
	// 1. Temp directory creation (above) is still parallel
	// 2. The actual rebase validation (after worktree creation) is still parallel
	// 3. Only the brief `git worktree` commands are serialized
	e.worktreeMu.Lock()
	e.maybePruneTempWorktreesLocked(ctx, prune)
	err = e.addWorktreeWithRetryLocked(ctx, worktreePath, branch, git.WorktreeDetached, checkout == WorktreeCheckoutShallow)
	e.worktreeMu.Unlock()

	if err != nil {
		_ = os.RemoveAll(tmpDir)
		return "", nil, fmt.Errorf("failed to add worktree: %w", err)
	}

	cleanup := func() {
		// Fast cleanup: remove directories and defer pruning until next creation.
		e.worktreeMu.Lock()
		e.tempWorktreeNeedsPrune = true
		e.worktreeMu.Unlock()

		_ = os.RemoveAll(worktreePath)
		_ = os.RemoveAll(tmpDir)
	}

	return worktreePath, cleanup, nil
}

// maybePruneTempWorktreesLocked prunes stale temp worktrees if needed.
// Caller must hold worktreeMu.
func (e *engineImpl) maybePruneTempWorktreesLocked(ctx context.Context, prune WorktreePruneMode) {
	if prune == WorktreePruneSkip && !e.tempWorktreeNeedsPrune {
		return
	}

	if prune == WorktreePruneAuto && !e.tempWorktreeNeedsPrune && e.tempWorktreePrunedOnce {
		return
	}

	if err := e.git.PruneWorktrees(ctx); err != nil {
		return
	}

	e.tempWorktreeNeedsPrune = false
	e.tempWorktreePrunedOnce = true
}

// addWorktreeWithRetryLocked attempts to add a worktree, pruning and retrying once on failure.
// Caller must hold worktreeMu.
func (e *engineImpl) addWorktreeWithRetryLocked(ctx context.Context, path string, branch string, detach git.WorktreeDetachMode, noCheckout bool) error {
	if err := e.git.AddWorktreeWithOptions(ctx, path, branch, detach, noCheckout); err == nil {
		return nil
	}

	pruneErr := e.git.PruneWorktrees(ctx)
	if pruneErr == nil {
		e.tempWorktreeNeedsPrune = false
		e.tempWorktreePrunedOnce = true
	} else {
		e.tempWorktreeNeedsPrune = true
	}

	retryErr := e.git.AddWorktreeWithOptions(ctx, path, branch, detach, noCheckout)
	if retryErr == nil {
		return nil
	}

	if pruneErr != nil {
		return fmt.Errorf("failed to add worktree at %s after prune attempt (%w): %w", path, pruneErr, retryErr)
	}
	return fmt.Errorf("failed to add worktree at %s after prune retry: %w", path, retryErr)
}

// RegisterWorktree registers a worktree for a stack root in local git refs
func (e *engineImpl) RegisterWorktree(stackRoot string, path string) error {
	return e.RegisterWorktreeWithName(stackRoot, path, "")
}

// RegisterWorktreeWithName registers a worktree with a user-friendly name
func (e *engineImpl) RegisterWorktreeWithName(anchorBranch string, path string, name string) error {
	absPath, err := canonicalWorktreePath(path)
	if err != nil {
		return fmt.Errorf("failed to canonicalize worktree path: %w", err)
	}

	if existing, err := e.GetWorktreeForStack(anchorBranch); err != nil {
		return fmt.Errorf("failed to check existing worktree registration: %w", err)
	} else if existing != nil {
		return fmt.Errorf("worktree anchor %s is already registered at %s", anchorBranch, existing.Path)
	}

	worktrees, err := e.ListManagedWorktrees()
	if err != nil {
		return fmt.Errorf("failed to list worktree registrations: %w", err)
	}
	for _, worktree := range worktrees {
		worktreePath, pathErr := canonicalWorktreePath(worktree.Path)
		if pathErr != nil {
			return fmt.Errorf("failed to canonicalize registered worktree path %s: %w", worktree.Path, pathErr)
		}
		if worktreePath == absPath && worktree.AnchorBranch != anchorBranch {
			return fmt.Errorf("worktree path %s is already registered to anchor %s", absPath, worktree.AnchorBranch)
		}
	}

	meta := &git.WorktreeMeta{
		Name:         name,
		Path:         absPath,
		AnchorBranch: anchorBranch,
		CreatedAt:    timeNow(),
		MainRepoDir:  e.repoRoot,
	}

	return e.git.WriteWorktreeMeta(anchorBranch, meta)
}

func canonicalWorktreePath(path string) (string, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolvedPath, err := filepath.EvalSymlinks(absPath)
	if err == nil {
		return filepath.Clean(resolvedPath), nil
	}
	if !os.IsNotExist(err) {
		return "", err
	}
	return filepath.Clean(absPath), nil
}

// UnregisterWorktree removes worktree registration for a stack root
func (e *engineImpl) UnregisterWorktree(ctx context.Context, stackRoot string) error {
	return e.git.DeleteWorktreeMeta(ctx, stackRoot)
}

// GetWorktreeForStack returns worktree info for a stack root, or nil if none
func (e *engineImpl) GetWorktreeForStack(stackRoot string) (*WorktreeInfo, error) {
	meta, err := e.git.ReadWorktreeMeta(stackRoot)
	if err != nil {
		return nil, err
	}
	if meta == nil {
		return nil, nil
	}

	return &WorktreeInfo{
		Name:         meta.Name,
		Path:         meta.Path,
		AnchorBranch: meta.AnchorBranch,
		CreatedAt:    meta.CreatedAt,
		MainRepoDir:  meta.MainRepoDir,
	}, nil
}

// OwningWorktree returns the managed worktree for branch's stack. Stack
// ownership is derived from the stack root, which is the worktree anchor for
// stacks that have been attached to a managed worktree.
func (e *engineImpl) OwningWorktree(branch Branch) (*WorktreeInfo, error) {
	stackRoot := e.GetStackRootForBranch(branch)
	if stackRoot == "" {
		return nil, nil
	}

	worktree, err := e.GetWorktreeForStack(stackRoot)
	if err != nil || worktree == nil {
		return worktree, err
	}
	if worktree.AnchorBranch != stackRoot {
		return nil, fmt.Errorf("invalid worktree registration for stack %s: metadata anchor is %s", stackRoot, worktree.AnchorBranch)
	}

	return worktree, nil
}

// ListManagedWorktrees returns all stackit-managed worktrees, sorted by stack root name
func (e *engineImpl) ListManagedWorktrees() ([]WorktreeInfo, error) {
	metas, err := e.git.ListWorktreeMetas()
	if err != nil {
		return nil, err
	}

	// Sort keys for deterministic output
	keys := make([]string, 0, len(metas))
	for k := range metas {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	result := make([]WorktreeInfo, 0, len(metas))
	for _, k := range keys {
		meta := metas[k]
		result = append(result, WorktreeInfo{
			Name:         meta.Name,
			Path:         meta.Path,
			AnchorBranch: meta.AnchorBranch,
			CreatedAt:    meta.CreatedAt,
			MainRepoDir:  meta.MainRepoDir,
		})
	}

	return result, nil
}

// GetStackRootForBranch returns the stack root for a given branch.
// The stack root is the first ancestor branch whose parent is trunk.
// Returns empty string for trunk or untracked branches.
func (e *engineImpl) GetStackRootForBranch(branch Branch) string {
	branchName := branch.GetName()

	// Trunk has no stack root
	e.mu.RLock()
	trunk := e.trunk
	e.mu.RUnlock()
	if branchName == trunk {
		return ""
	}

	current := branchName
	visited := make(map[string]bool)
	for {
		if visited[current] {
			return ""
		}
		visited[current] = true
		e.ensureBranchSharedLoaded(current)

		e.mu.RLock()
		state := e.readState(current)
		trunk := e.trunk
		parent := ""
		if state != nil {
			parent = state.Parent
		}
		e.mu.RUnlock()

		if state == nil {
			// Should not happen since we checked above, but handle gracefully
			return ""
		}

		// If parent is trunk, current is the stack root
		if parent == trunk {
			return current
		}

		current = parent
	}
}

// IsInManagedWorktree checks if the current directory is a stackit-managed worktree.
// Returns true and worktree info if in a managed worktree, false otherwise.
func (e *engineImpl) IsInManagedWorktree() (bool, *WorktreeInfo, error) {
	// Check if .git is a file (worktree) vs directory (main repo)
	gitPath := filepath.Join(e.repoRoot, ".git")
	info, err := os.Stat(gitPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil, nil // Not in a git repo
		}
		return false, nil, fmt.Errorf("failed to stat .git: %w", err)
	}

	// If .git is a directory, we're in the main repo, not a worktree
	if info.IsDir() {
		return false, nil, nil
	}

	// .git is a file - we're in a worktree. Now check if it's stackit-managed.
	// Get the current working directory (worktree path)
	currentPath, err := filepath.Abs(e.repoRoot)
	if err != nil {
		return false, nil, fmt.Errorf("failed to get absolute path: %w", err)
	}

	// List all managed worktrees and check if current path matches
	worktrees, err := e.ListManagedWorktrees()
	if err != nil {
		return false, nil, fmt.Errorf("failed to list managed worktrees: %w", err)
	}

	for _, wt := range worktrees {
		// Compare paths (normalize both)
		wtPath, err := filepath.Abs(wt.Path)
		if err != nil {
			continue
		}
		if wtPath == currentPath {
			return true, &WorktreeInfo{
				Name:         wt.Name,
				Path:         wt.Path,
				AnchorBranch: wt.AnchorBranch,
				CreatedAt:    wt.CreatedAt,
				MainRepoDir:  wt.MainRepoDir,
			}, nil
		}
	}

	// It's a worktree but not managed by stackit
	return false, nil, nil
}
