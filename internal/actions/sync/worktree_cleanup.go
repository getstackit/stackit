package sync

import (
	"context"
	"time"

	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/git"
	worktreeutil "github.com/getstackit/stackit/internal/worktree"
)

// WorktreeCleanupResult contains the results of worktree cleanup
type WorktreeCleanupResult struct {
	RemovedWorktrees []string // Stack roots whose worktrees were removed
	Errors           []string // Any errors encountered (non-fatal)
}

// cleanOrphanedWorktrees removes worktrees for stacks that no longer exist.
// This is called during sync after branch cleanup to remove worktrees for merged/deleted stacks.
// If a stack root is deleted but has surviving children (reparented to trunk), the worktree
// registry is updated to point to the new stack root instead of deleting the worktree.
// This function is best-effort and will not fail sync on errors.
// Worktrees with dirty anchors (uncommitted changes) are skipped to preserve work in progress.
func cleanOrphanedWorktrees(ctx context.Context, deps Deps, dirtyAnchors dirtyAnchorSet) *WorktreeCleanupResult {
	result := &WorktreeCleanupResult{
		RemovedWorktrees: []string{},
		Errors:           []string{},
	}

	// Check if auto-clean is enabled, using the config resolved at bootstrap.
	// Without a loaded config, skip cleanup rather than fail sync.
	if deps.Config == nil {
		deps.Out.Debug("No repository config loaded; skipping worktree cleanup")
		return result
	}

	if !deps.Config.WorktreeAutoClean() {
		deps.Out.Debug("Worktree auto-clean is disabled")
		return result
	}

	// Get all managed worktrees
	listStart := time.Now()
	worktrees, err := deps.Engine.ListManagedWorktrees(ctx)
	deps.Log.Info("list managed worktrees completed durationMs=%d worktreeCount=%d", time.Since(listStart).Milliseconds(), len(worktrees))
	if err != nil {
		deps.Out.Debug("Failed to list managed worktrees: %v", err)
		return result
	}

	if len(worktrees) == 0 {
		return result
	}

	graph := deps.Engine.Graph(ctx, engine.SortStrategyAlphabetical)

	// Check each anchored worktree and remove it once it is clean and empty.
	for _, wt := range worktrees {
		// The main worktree is never disposable, even if an invalid
		// registration makes it look like an empty managed worktree. Skipping
		// this candidate lets cleanup continue for the rest of the batch.
		if git.IsMainWorktree(wt.Path, wt.MainRepoDir) {
			deps.Out.Debug("Skipping cleanup for main worktree %s", wt.Path)
			continue
		}

		// Skip dirty worktrees - don't clean up while there are uncommitted changes
		if dirtyAnchors[wt.AnchorBranch] {
			continue
		}
		if deps.Here.InManagedWorktree && deps.Here.Worktree != nil && wt.AnchorBranch == deps.Here.Worktree.AnchorBranch {
			deps.Out.Debug("Skipping cleanup for current managed worktree %s", wt.AnchorBranch)
			continue
		}

		anchorBranch := deps.Engine.GetBranch(wt.AnchorBranch)
		anchorExists := deps.Engine.BranchNames().Contains(wt.AnchorBranch)
		if anchorExists && !deps.Engine.IsWorktreeAnchor(anchorBranch) {
			result.Errors = append(result.Errors, "worktree "+wt.AnchorBranch+" is registered to a non-anchor branch")
			continue
		}

		if anchorExists && len(graph.Children(anchorBranch)) > 0 {
			continue
		}

		deps.Out.Info("Removing empty worktree %s", wt.AnchorBranch)

		_, removeErr := worktreeutil.RemovePath(ctx, deps.Engine, wt.Path, worktreeutil.RemovalRespectChanges)
		if removeErr != nil {
			result.Errors = append(result.Errors,
				"failed to remove worktree at "+wt.Path.String()+": "+removeErr.Error())
			deps.Out.Debug("Failed to remove worktree at %s: %v", wt.Path, removeErr)
			continue
		}

		// Removing a directory can leave Git's administrative worktree entry
		// behind. Prune it before deleting the anchor; an anchor deleted first
		// is not recoverable from a later unregister failure.
		if pruneErr := deps.Engine.PruneWorktrees(ctx); pruneErr != nil {
			result.Errors = append(result.Errors,
				"failed to prune stale worktree entries for "+wt.AnchorBranch+": "+pruneErr.Error())
			deps.Out.Debug("Failed to prune stale worktree entries for %s: %v", wt.AnchorBranch, pruneErr)
			continue
		}

		if anchorExists {
			if deleteErr := deps.Engine.DeleteBranch(ctx, anchorBranch); deleteErr != nil {
				result.Errors = append(result.Errors,
					"failed to delete anchor branch "+wt.AnchorBranch+": "+deleteErr.Error())
				deps.Out.Debug("Failed to delete anchor branch %s: %v", wt.AnchorBranch, deleteErr)
				continue
			}
		}

		if unregErr := deps.Engine.UnregisterWorktree(ctx, wt.AnchorBranch); unregErr != nil {
			result.Errors = append(result.Errors,
				"failed to unregister worktree for "+wt.AnchorBranch+": "+unregErr.Error())
			deps.Out.Debug("Failed to unregister worktree for %s: %v", wt.AnchorBranch, unregErr)
			continue
		}
		result.RemovedWorktrees = append(result.RemovedWorktrees, wt.AnchorBranch)
	}

	return result
}
