package merge

import (
	"fmt"

	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/engine"
)

// ExecuteInWorktree executes the merge plan in a temporary worktree
func ExecuteInWorktree(ctx *app.Context, eng mergeExecuteEngine, opts ExecuteOptions, scope string, targetBranch string) (err error) {
	out := ctx.Output

	// Create temporary worktree via engine
	// We use detached HEAD at the current revision to avoid "already used by worktree" errors
	// and to ensure we don't accidentally move any main workspace branch refs.
	worktreePath, cleanup, err := eng.CreateTemporaryWorktree(ctx.Context, "HEAD", "stackit-merge-*")
	if err != nil {
		return err
	}

	out.Debug("📁 Worktree: %s", worktreePath)

	// Ensure we clean up on exit
	cleanupWorktree := true
	defer func() {
		if cleanupWorktree {
			out.Debug("Cleaning up worktree at %s", worktreePath)
			cleanup()
		}
	}()

	// 3. Create a new engine for the worktree
	maxUndoDepth := opts.UndoStackDepth
	if maxUndoDepth <= 0 {
		maxUndoDepth = engine.DefaultMaxUndoStackDepth
	}

	// We need to know the trunk name for the new engine.
	// Since we are currently in the main engine, we can get it from there.
	trunk := eng.Trunk()

	worktreeEng, err := engine.NewEngine(engine.Options{
		RepoRoot:          worktreePath,
		Trunk:             trunk.GetName(),
		MaxUndoStackDepth: maxUndoDepth,
	})
	if err != nil {
		return fmt.Errorf("failed to initialize engine in worktree: %w", err)
	}

	// Create a sub-context for the worktree.
	worktreeCtx := *ctx //nolint:govet // Copying Context by value is intentional when overriding engine/repo output fields.
	worktreeCtx.Engine = worktreeEng
	worktreeCtx.RepoRoot = worktreePath

	// 4. Pre-flight operations in the worktree
	// Pull trunk in the worktree to ensure we have latest changes
	pullResult, err := worktreeEng.PullTrunk(ctx.Context)
	if err != nil {
		out.Debug("Failed to pull trunk in worktree: %v", err)
	} else if pullResult == engine.PullConflict {
		if opts.Force {
			out.Info("Trunk diverged from remote. Force-resetting trunk to match remote...")
			if err := worktreeEng.ResetTrunkToRemote(ctx.Context); err != nil {
				return fmt.Errorf("failed to auto-reset diverged trunk: %w", err)
			}
		} else {
			return fmt.Errorf("trunk could not be fast-forwarded (diverged from remote). This usually happens when local trunk has commits not on remote. To fix:\n  1. Sync your trunk: git checkout %s && git pull\n  2. Or use --force to overwrite local trunk changes", trunk.GetName())
		}
	}

	// 5. Create or Validate the plan
	plan := opts.Plan
	if plan == nil {
		// Create plan in worktree
		var err error
		plan, _, err = CreateMergePlan(ctx.Context, worktreeEng, out, ctx.GitHub(), CreatePlanOptions{
			Strategy:     opts.Strategy,
			Force:        opts.Force,
			Scope:        scope,
			TargetBranch: targetBranch,
		})
		if err != nil {
			return err
		}

		// Update opts with the new plan
		opts.Plan = plan
	}

	// 6. Execute the plan in the worktree
	err = Execute(&worktreeCtx, worktreeEng, opts)

	if err != nil {
		// If it's a conflict, don't clean up so the user can resolve it
		if isConflictError(err) {
			cleanupWorktree = false
			out.Warn("Conflict detected during merge execution in worktree.")
			out.Info("The worktree has been preserved for manual resolution.")
			out.Info("Your main workspace has been left untouched.")
			out.Newline()
			out.Info("To resolve the conflict and continue:")
			out.Info("  1. cd %s", worktreePath)
			out.Info("  2. Resolve the conflicts and git add the files.")
			out.Info("  3. Run 'stackit continue' to finish the merge/restack.")
			out.Info("  4. Once finished, return to your main workspace.")
			return err
		}

		// For other errors (like CI failure), we still want to give instructions
		// but we can clean up the worktree.
		out.Warn("Merge execution failed in worktree.")
		out.Info("Your main workspace remains untouched.")
		out.Newline()
		if isCIFailure(err) {
			out.Info("CI checks failed. Please:")
			out.Info("  1. Fix the issues in your main workspace.")
			out.Info("  2. Run 'stackit submit' to update PRs.")
			out.Info("  3. Run 'stackit merge' again once CI passes.")
		} else {
			out.Info("To resolve:")
			out.Info("  1. Fix the underlying issue in your main workspace.")
			out.Info("  2. Run 'stackit merge' again.")
		}
		return err
	}

	return nil
}
