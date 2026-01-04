package actions

import (
	"fmt"

	"stackit.dev/stackit/internal/app"
	"stackit.dev/stackit/internal/config"
	"stackit.dev/stackit/internal/engine"
	"stackit.dev/stackit/internal/git"
	"stackit.dev/stackit/internal/tui/style"
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
			return fmt.Errorf("not on a branch")
		}
		parent := currentBranch.GetParent()
		parentName := ""
		if parent == nil {
			parentName = eng.Trunk().GetName()
		} else {
			parentName = parent.GetName()
		}
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
			if currentBranch != nil {
				branchName = currentBranch.GetName()
			}
		}
		if branchName != "" {
			if err := PrintConflictStatus(ctx, branchName); err != nil {
				return fmt.Errorf("failed to print conflict status: %w", err)
			}
		} else {
			out.Warn("Hit rebase conflict, but could not determine current branch name.")
		}
		return fmt.Errorf("rebase conflict is not yet resolved")
	}

	// Success - inform user
	if result.BranchName != "" {
		out.Info("Resolved rebase conflict for %s.", style.ColorBranchName(result.BranchName, true))
	} else {
		out.Info("Resolved rebase conflict.")
	}

	// Continue with remaining branches to restack
	if len(continuation.BranchesToRestack) > 0 {
		// Convert []string to []Branch for RestackBranches
		branches := make([]engine.Branch, 0, len(continuation.BranchesToRestack))
		for _, name := range continuation.BranchesToRestack {
			branch := eng.GetBranch(name)
			branches = append(branches, branch)
		}
		if len(branches) > 0 {
			if err := RestackBranches(ctx, branches); err != nil {
				return err
			}
		}
	}

	// Clear continuation state
	if err := config.ClearContinuationState(ctx.RepoRoot); err != nil {
		out.Debug("Failed to clear continuation state: %v", err)
	}

	return nil
}
