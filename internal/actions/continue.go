package actions

import (
	"fmt"

	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/config"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/errors"
	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/internal/output"
)

// ContinueOptions contains options for the continue command
type ContinueOptions struct {
	AddAll bool
}

// ContinueAction performs the continue operation
func ContinueAction(ctx *app.Context, opts ContinueOptions) error {
	eng := ctx.Engine
	out := ctx.Output

	// Check if rebase is in progress
	if !eng.IsRebaseInProgress(ctx.Context) {
		// Clear any stale continuation state
		_ = config.ClearContinuationState(ctx.RepoRoot)
		return fmt.Errorf("no rebase in progress. Nothing to continue")
	}

	// Load continuation state
	continuation, err := config.GetContinuationState(ctx.RepoRoot)
	if err != nil {
		// No continuation state - this is okay, we can still continue the rebase
		// but we won't be able to resume restacking
		out.Info("No continuation state found. Continuing rebase only.")
		// Try to continue the rebase anyway (user might have started it manually)
		// But we need a rebasedBranchBase - try to get it from current branch's parent
		currentBranch := eng.CurrentBranch()
		if currentBranch == nil {
			return errors.ErrNotOnBranch
		}
		parentName := currentBranch.GetParentOrTrunk()
		parentBranch := eng.GetBranch(parentName)
		parentRev, err := parentBranch.GetRevision()
		if err != nil {
			return fmt.Errorf("failed to get parent revision: %w", err)
		}
		continuation = &config.ContinuationState{
			RebasedBranchBase:     parentRev,
			BranchesToRestack:     []string{},
			CurrentBranchOverride: currentBranch.GetName(),
		}
		expectedBranchRevision, err := currentBranch.GetRevision()
		if err != nil {
			return fmt.Errorf("failed to get current branch revision: %w", err)
		}
		continuation.ExpectedBranchRevision = expectedBranchRevision
	}

	// Stage all changes if --all flag is set
	if opts.AddAll {
		if err := eng.StageAll(ctx.Context); err != nil {
			return fmt.Errorf("failed to stage changes: %w", err)
		}
	}

	// Continue the rebase
	result, err := eng.ContinueRebase(ctx.Context, continuation.CurrentBranchOverride, continuation.RebasedBranchBase, continuation.ExpectedBranchRevision)
	if err != nil {
		return fmt.Errorf("failed to continue rebase: %w", err)
	}

	// Handle result
	if result.Result == int(git.RebaseConflict) {
		// Another conflict - persist state again
		if err := config.PersistContinuationState(ctx.RepoRoot, continuation); err != nil {
			return fmt.Errorf("failed to persist continuation: %w", err)
		}
		// Get current branch name for conflict status
		branchName := result.BranchName
		if branchName == "" {
			currentBranch := eng.CurrentBranch()
			if currentBranch == nil {
				return errors.ErrNotOnBranch
			}
			branchName = currentBranch.GetName()
		}
		if result.RerereResolvedCount > 0 {
			printRerereResolved(ctx, result.RerereResolvedCount)
		}
		if err := PrintConflictStatus(ctx, branchName); err != nil {
			return fmt.Errorf("failed to print conflict status: %w", err)
		}
		// PrintConflictStatus already told the user what to do; return the
		// silenced conflict-workflow error so cobra doesn't reprint it.
		return errors.NewConflictWorkflowError(branchName)
	}

	// Success - checkout the branch (rebase leaves us in detached HEAD).
	//
	// A branch checked out in another worktree cannot be attached here, and
	// since CheckoutBranch stopped falling back to a detached checkout that is
	// now a hard error. Failing the whole command for it abandons the branches
	// still queued in BranchesToRestack and leaves the continuation state on
	// disk, so the resolved conflict has to be redone. Carry on with the rest of
	// the stack instead — but do not leave the user silently detached, which is
	// where `git rebase` left HEAD and how commits get orphaned.
	attached := true
	if err := eng.CheckoutBranch(ctx.Context, eng.GetBranch(result.BranchName)); err != nil {
		otherWorktree := ""
		if worktrees, listErr := eng.ListWorktrees(ctx.Context); listErr == nil {
			otherWorktree = worktrees.PathForBranch(result.BranchName)
		}
		if otherWorktree == "" {
			return fmt.Errorf("failed to checkout branch %s: %w", result.BranchName, err)
		}
		attached = false
		out.Warn("Updated %s, but it is checked out in worktree %s so it cannot be checked out here: %v",
			result.BranchName, otherWorktree, err)
	}

	if attached {
		out.Info("Resolved rebase conflict for %s.", output.CurrentBranch(result.BranchName))
	} else {
		out.Info("Resolved rebase conflict for %s.", output.BranchName(result.BranchName))
	}
	if result.RerereResolvedCount > 0 {
		printRerereResolved(ctx, result.RerereResolvedCount)
	}
	reportUnresetWorktree(out, result)

	// Continue with remaining branches to restack
	if len(continuation.BranchesToRestack) > 0 {
		branches := engine.BranchesFromNames(eng, continuation.BranchesToRestack)
		if err := RestackBranches(ctx, branches); err != nil {
			return err
		}
	}

	// Return to the branch the interrupted command was invoked from, when it
	// differs from the one that was rebased. `modify --into` records this: it
	// amends a downstack ancestor without checking it out and promises to put
	// you back, and resolving a conflict along the way must not change where
	// you end up. Unset for every other command, which correctly leaves the
	// user on the branch they just resolved.
	if continuation.ReturnToBranch != "" && continuation.ReturnToBranch != result.BranchName {
		if err := eng.CheckoutBranch(ctx.Context, eng.GetBranch(continuation.ReturnToBranch)); err == nil {
			attached = true
		} else {
			out.Error("Failed to return to %s: %v", continuation.ReturnToBranch, err)
		}
	}

	// The rebase left HEAD detached and the branch could not be checked out
	// here, so land on a real branch rather than returning success from a
	// detached HEAD, where the user's next commit belongs to nothing. Trunk is
	// the one branch that is always present; if it too is held elsewhere, say
	// plainly where the user ended up instead of implying they are on a branch.
	if !attached {
		trunk := eng.Trunk()
		if err := eng.CheckoutBranch(ctx.Context, trunk); err == nil {
			out.Info("Switched to %s; %s lives in its own worktree.",
				output.BranchName(trunk.GetName()), output.BranchName(result.BranchName))
		} else {
			out.Warn("HEAD is detached here. Run 'stackit checkout <branch>' to get back onto a branch.")
		}
	}

	// Clear continuation state
	if err := config.ClearContinuationState(ctx.RepoRoot); err != nil {
		out.Debug("Failed to clear continuation state: %v", err)
	}

	return nil
}

// reportUnresetWorktree tells the user about a worktree left holding pre-rebase
// content under a ref that has since moved.
//
// Nothing else will surface this. The worktree is not the one they are standing
// in, `git status` there reports the rebased commit's files as deleted, and the
// next `stackit modify -a` run in it commits that deletion — silently reverting
// the rebase they just resolved. The remedy lives in a directory they are not
// looking at, so name it.
func reportUnresetWorktree(out output.Output, result engine.ContinueRebaseResult) {
	if result.UnresetWorktree == "" {
		return
	}
	out.Warn("Worktree %s still has %s checked out at the pre-rebase content because %s.",
		result.UnresetWorktree, result.BranchName, result.UnresetReason)
	out.Warn("Commit or discard the changes there, then run 'git reset --hard %s' in that worktree.",
		result.BranchName)
}
