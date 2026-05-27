package merge

import (
	"context"
	"fmt"

	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/github"
)

// validateStepPreconditions validates that a step can be executed
func validateStepPreconditions(ctx context.Context, step PlanStep, eng mergeExecuteEngine, githubClient github.Client, opts ExecuteOptions) error {
	switch step.StepType {
	case StepMergePR:
		// Validate PR still exists and is open
		branch := eng.GetBranch(step.BranchName)
		prInfo, err := branch.GetPrInfo()
		if err != nil {
			return fmt.Errorf("failed to get PR info: %w", err)
		}
		if prInfo == nil || prInfo.Number() == nil {
			return fmt.Errorf("PR not found for branch %s", step.BranchName)
		}
		if prInfo.State() != prStateOpen {
			return fmt.Errorf("PR #%d for branch %s is %s (not open)", *prInfo.Number(), step.BranchName, prInfo.State())
		}
		// Optionally check CI checks haven't changed to failing or pending
		if !opts.Force && githubClient != nil {
			status, err := githubClient.GetPRChecksStatus(ctx, step.BranchName)
			if err == nil && status != nil {
				if !status.Passing {
					return fmt.Errorf("PR #%d for branch %s has failing CI checks", *prInfo.Number(), step.BranchName)
				}
				if status.Pending {
					return fmt.Errorf("PR #%d for branch %s has pending CI checks", *prInfo.Number(), step.BranchName)
				}
			}
		}

	case StepRestack:
		// Validate branch still exists
		branch := eng.GetBranch(step.BranchName)
		if !branch.IsTracked() {
			return fmt.Errorf("branch %s is not tracked", step.BranchName)
		}

	case StepDeleteBranch:
		// Validate branch exists (or allow if already deleted)
		// This is non-blocking - branch might already be deleted

	case StepUpdatePRBase:
		// Validate PR exists
		branch := eng.GetBranch(step.BranchName)
		prInfo, err := branch.GetPrInfo()
		if err != nil {
			return fmt.Errorf("failed to get PR info: %w", err)
		}
		if prInfo == nil || prInfo.Number() == nil {
			return fmt.Errorf("PR not found for branch %s", step.BranchName)
		}

	case StepPullTrunk:
		// No preconditions needed

	case StepWaitCI:
		// Validate PR exists and is open
		branch := eng.GetBranch(step.BranchName)
		prInfo, err := branch.GetPrInfo()
		if err != nil {
			return fmt.Errorf("failed to get PR info: %w", err)
		}
		if prInfo == nil || prInfo.Number() == nil {
			return fmt.Errorf("PR not found for branch %s", step.BranchName)
		}
		if prInfo.State() != prStateOpen {
			return fmt.Errorf("PR #%d for branch %s is %s (not open)", *prInfo.Number(), step.BranchName, prInfo.State())
		}
	}

	return nil
}

// executeUpdatePRBase handles the UPDATE_PR_BASE step
// This is used in top-down strategy to rebase the current branch onto trunk
func executeUpdatePRBase(ctx *app.Context, eng mergeExecuteEngine, step PlanStep) error {
	trunk := eng.Trunk()
	trunkName := trunk.GetName()
	githubClient := ctx.GitHub()

	// Get the parent revision (old base)
	branch := eng.GetBranch(step.BranchName)
	parentName := branch.GetParentOrTrunk()

	// Get the old parent revision
	parentBranch := eng.GetBranch(parentName)
	oldParentRev, err := parentBranch.GetRevision()
	if err != nil {
		return fmt.Errorf("failed to get parent revision: %w", err)
	}

	// If parent is already trunk, we might just need to update the PR base
	if parentName == trunkName {
		// Just update the PR base branch via GitHub API
		return updatePRBaseBranchFromContext(ctx.Context, githubClient, step.BranchName, trunkName)
	}

	// Rebase the branch onto trunk
	// Save current branch state since git.Rebase no longer does this
	currentBranchObj := eng.CurrentBranch()
	currentBranch := ""
	if currentBranchObj != nil {
		currentBranch = currentBranchObj.GetName()
	}
	var currentRev string
	if currentBranch == "" {
		currentRev, _ = eng.GetCurrentRevision(ctx.Context)
	}

	ctx.Output.Debug("Rebasing %s onto trunk %s (old base %s)", step.BranchName, trunkName, oldParentRev)
	gitResult, err := eng.Rebase(ctx.Context, step.BranchName, trunkName, oldParentRev)

	// Restore original branch state
	if currentBranch != "" {
		_ = eng.CheckoutBranch(ctx.Context, eng.GetBranch(currentBranch))
	} else if currentRev != "" {
		_ = eng.Detach(ctx.Context, currentRev)
	}

	if err != nil {
		ctx.Output.Debug("Rebase of %s onto %s failed: %v", step.BranchName, trunkName, err)
		return fmt.Errorf("failed to rebase %s onto %s: %w", step.BranchName, trunkName, err)
	}

	if gitResult == engine.RestackConflict {
		return fmt.Errorf("rebase conflict while rebasing %s onto %s", step.BranchName, trunkName)
	}

	// Update parent to trunk
	if err := eng.SetParent(ctx.Context, eng.GetBranch(step.BranchName), eng.GetBranch(trunkName), engine.DivergenceRecompute); err != nil {
		return fmt.Errorf("failed to update parent: %w", err)
	}

	// Update PR base branch via GitHub API
	if err := updatePRBaseBranchFromContext(ctx.Context, githubClient, step.BranchName, trunkName); err != nil {
		return fmt.Errorf("failed to update PR base: %w", err)
	}

	// Rebuild engine to reflect changes
	if err := eng.Rebuild(trunkName); err != nil {
		return fmt.Errorf("failed to rebuild engine: %w", err)
	}

	return nil
}

// updatePRBaseBranchFromContext updates a PR's base branch via GitHub API
func updatePRBaseBranchFromContext(ctx context.Context, githubClient github.Client, branchName, newBase string) error {
	if githubClient == nil {
		// If we can't get GitHub client, skip this step (non-fatal)
		return nil
	}

	owner, repo := githubClient.GetOwnerRepo()

	// Get PR for this branch
	pr, err := githubClient.GetPullRequestByBranch(ctx, owner, repo, branchName)
	if err != nil || pr == nil {
		// PR not found or error - non-fatal
		return nil //nolint:nilerr
	}

	// Update PR base
	updateOpts := github.UpdatePROptions{
		Base: &newBase,
	}

	if _, err := githubClient.UpdatePullRequest(ctx, owner, repo, pr.Number, updateOpts); err != nil {
		return fmt.Errorf("failed to update PR base: %w", err)
	}

	return nil
}

// executeConsolidation handles the stack consolidation process
func executeConsolidation(ctx *app.Context, eng mergeExecuteEngine, stepIndex int, opts ExecuteOptions) (*ConsolidationResult, error) {
	consolidator := NewConsolidateMergeExecutor(opts.Plan, eng, ctx)
	consolidator.SetProgressHandler(opts.Handler, stepIndex)
	consolidator.mergeMethod = opts.MergeMethod
	result, err := consolidator.Execute(ctx.Context, opts)
	if err != nil {
		return nil, err
	}

	return result, nil
}

// executeWaitCIWithProgress waits for CI checks with progress reporting
func executeWaitCIWithProgress(ctx *app.Context, step PlanStep, stepIndex int, eng mergeExecuteEngine, opts ExecuteOptions) error {
	// Get PR info for better error messages
	branch := eng.GetBranch(step.BranchName)
	prInfo, err := branch.GetPrInfo()
	if err != nil {
		return fmt.Errorf("failed to get PR info: %w", err)
	}
	prNumber := step.PRNumber
	if prInfo != nil && prInfo.Number() != nil {
		prNumber = *prInfo.Number()
	}

	timeout := step.WaitTimeout
	if timeout == 0 {
		timeout = DefaultCITimeout
	}

	waiter := NewCIWaiter(CIWaiterOptions{
		Client:  ctx.GitHub(),
		Output:  ctx.Output,
		Timeout: timeout,
	})
	waiter.SetProgressHandler(opts.Handler, stepIndex)

	result, err := waiter.WaitForChecks(ctx.Context, step.BranchName, prNumber, step.ExpectChecks)
	if err != nil {
		return fmt.Errorf("CI checks failed on PR #%d (%s): %w", prNumber, step.BranchName, err)
	}

	// Set estimated duration for future progress reporting
	if result.MaxDuration > 0 {
		opts.Handler.EmitEvent(Event{
			Type:              EventProgress,
			EstimatedDuration: result.MaxDuration,
		})
	}

	return nil
}
