package sync

import (
	"os"
	"time"

	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/config"
	"github.com/getstackit/stackit/internal/engine"
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
func cleanOrphanedWorktrees(ctx *app.Context, dirtyAnchors map[string]bool) *WorktreeCleanupResult {
	result := &WorktreeCleanupResult{
		RemovedWorktrees: []string{},
		Errors:           []string{},
	}

	// Check if auto-clean is enabled
	configStart := time.Now()
	cfg, err := config.LoadConfig(ctx.RepoRoot)
	ctx.Logger.Info("load config for worktree cleanup completed durationMs=%d", time.Since(configStart).Milliseconds())
	if err != nil {
		// If config can't be loaded, skip cleanup but don't fail
		ctx.Output.Debug("Failed to load config for worktree cleanup: %v", err)
		return result
	}

	if !cfg.WorktreeAutoClean() {
		ctx.Output.Debug("Worktree auto-clean is disabled")
		return result
	}

	// Get all managed worktrees
	listStart := time.Now()
	worktrees, err := ctx.Engine.ListManagedWorktrees()
	ctx.Logger.Info("list managed worktrees completed durationMs=%d worktreeCount=%d", time.Since(listStart).Milliseconds(), len(worktrees))
	if err != nil {
		ctx.Output.Debug("Failed to list managed worktrees: %v", err)
		return result
	}

	if len(worktrees) == 0 {
		return result
	}

	graph := ctx.Engine.Graph(engine.SortStrategyAlphabetical)

	// Check each anchored worktree and remove it once it is clean and empty.
	for _, wt := range worktrees {
		// Skip dirty worktrees - don't clean up while there are uncommitted changes
		if dirtyAnchors[wt.AnchorBranch] {
			continue
		}
		if ctx.InManagedWorktree && ctx.WorktreeInfo != nil && wt.AnchorBranch == ctx.WorktreeInfo.AnchorBranch {
			ctx.Output.Debug("Skipping cleanup for current managed worktree %s", wt.AnchorBranch)
			continue
		}

		anchorBranch := ctx.Engine.GetBranch(wt.AnchorBranch)
		anchorExists := ctx.Engine.BranchNames().Contains(wt.AnchorBranch)
		if anchorExists && !ctx.Engine.IsWorktreeAnchor(anchorBranch) {
			result.Errors = append(result.Errors, "worktree "+wt.AnchorBranch+" is registered to a non-anchor branch")
			continue
		}

		if anchorExists && len(graph.Children(anchorBranch)) > 0 {
			continue
		}

		ctx.Output.Info("Removing empty worktree %s", wt.AnchorBranch)

		if _, statErr := os.Stat(wt.Path); statErr == nil {
			if removeErr := ctx.Engine.RemoveWorktree(ctx.Context, wt.Path); removeErr != nil {
				result.Errors = append(result.Errors,
					"failed to remove worktree at "+wt.Path+": "+removeErr.Error())
				ctx.Output.Debug("Failed to remove worktree at %s: %v", wt.Path, removeErr)
				continue
			}
		} else if os.IsNotExist(statErr) {
		} else {
			result.Errors = append(result.Errors,
				"failed to inspect worktree at "+wt.Path+": "+statErr.Error())
			ctx.Output.Debug("Failed to stat worktree at %s: %v", wt.Path, statErr)
			continue
		}

		if unregErr := ctx.Engine.UnregisterWorktree(wt.AnchorBranch); unregErr != nil {
			result.Errors = append(result.Errors,
				"failed to unregister worktree for "+wt.AnchorBranch+": "+unregErr.Error())
			ctx.Output.Debug("Failed to unregister worktree for %s: %v", wt.AnchorBranch, unregErr)
			continue
		}

		if anchorExists {
			if deleteErr := ctx.Engine.DeleteBranch(ctx.Context, anchorBranch); deleteErr != nil {
				result.Errors = append(result.Errors,
					"failed to delete anchor branch "+wt.AnchorBranch+": "+deleteErr.Error())
				ctx.Output.Debug("Failed to delete anchor branch %s: %v", wt.AnchorBranch, deleteErr)
			}
		}
		result.RemovedWorktrees = append(result.RemovedWorktrees, wt.AnchorBranch)
	}

	return result
}
