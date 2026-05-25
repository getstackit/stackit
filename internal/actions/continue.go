package actions

import (
	"fmt"

	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/config"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/errors"
	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/internal/tui/style"
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
	if !eng.Git().IsRebaseInProgress(ctx.Context) {
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
	}

	// Stage all changes if --all flag is set
	if opts.AddAll {
		if err := eng.Git().StageAll(ctx.Context); err != nil {
			return fmt.Errorf("failed to stage changes: %w", err)
		}
	}

	// Continue the rebase
	result, err := eng.ContinueRebase(ctx.Context, continuation.CurrentBranchOverride, continuation.RebasedBranchBase)
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
		return fmt.Errorf("rebase conflict is not yet resolved")
	}

	// Success - checkout the branch (rebase leaves us in detached HEAD)
	if err := eng.Git().CheckoutBranch(ctx.Context, result.BranchName); err != nil {
		return fmt.Errorf("failed to checkout branch %s: %w", result.BranchName, err)
	}

	out.Info("Resolved rebase conflict for %s.", style.ColorBranchName(result.BranchName, true))
	if result.RerereResolvedCount > 0 {
		printRerereResolved(ctx, result.RerereResolvedCount)
	}

	// Continue with remaining branches to restack
	if len(continuation.BranchesToRestack) > 0 {
		branches := engine.BranchesFromNames(eng, continuation.BranchesToRestack)
		if err := RestackBranches(ctx, branches); err != nil {
			return err
		}
	}

	// Clear continuation state
	if err := config.ClearContinuationState(ctx.RepoRoot); err != nil {
		out.Debug("Failed to clear continuation state: %v", err)
	}

	return nil
}
