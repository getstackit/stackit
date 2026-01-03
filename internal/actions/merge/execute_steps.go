package merge

import (
	"context"
	"fmt"
	"time"

	"stackit.dev/stackit/internal/app"
	"stackit.dev/stackit/internal/engine"
	"stackit.dev/stackit/internal/github"
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
	githubClient := ctx.GitHubClient

	// Get the parent revision (old base)
	branch := eng.GetBranch(step.BranchName)
	parent := branch.GetParent()
	parentName := ""
	if parent == nil {
		parentName = trunkName
	} else {
		parentName = parent.GetName()
	}

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

	ctx.Splog.Debug("Rebasing %s onto trunk %s (old base %s)", step.BranchName, trunkName, oldParentRev)
	gitResult, err := eng.Rebase(ctx.Context, step.BranchName, trunkName, oldParentRev)

	// Restore original branch state
	if currentBranch != "" {
		_ = eng.CheckoutBranch(ctx.Context, eng.GetBranch(currentBranch))
	} else if currentRev != "" {
		_ = eng.Detach(ctx.Context, currentRev)
	}

	if err != nil {
		ctx.Splog.Debug("Rebase of %s onto %s failed: %v", step.BranchName, trunkName, err)
		return fmt.Errorf("failed to rebase %s onto %s: %w", step.BranchName, trunkName, err)
	}

	if gitResult == engine.RestackConflict {
		return fmt.Errorf("rebase conflict while rebasing %s onto %s", step.BranchName, trunkName)
	}

	// Update parent to trunk
	if err := eng.SetParent(ctx.Context, eng.GetBranch(step.BranchName), eng.GetBranch(trunkName)); err != nil {
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

	if err := githubClient.UpdatePullRequest(ctx, owner, repo, pr.Number, updateOpts); err != nil {
		return fmt.Errorf("failed to update PR base: %w", err)
	}

	return nil
}

// executeConsolidation handles the stack consolidation process
func executeConsolidation(ctx *app.Context, eng mergeExecuteEngine, opts ExecuteOptions) (*ConsolidationResult, error) {
	consolidator := NewConsolidateMergeExecutor(opts.Plan, eng, ctx)
	result, err := consolidator.Execute(ctx.Context, opts)
	if err != nil {
		return nil, err
	}

	return result, nil
}

// executeWaitCIWithProgress waits for CI checks with progress reporting
func executeWaitCIWithProgress(ctx *app.Context, step PlanStep, stepIndex int, eng mergeExecuteEngine, opts ExecuteOptions) error {
	githubClient := ctx.GitHubClient
	splog := ctx.Splog

	if githubClient == nil {
		return fmt.Errorf("GitHub client not available")
	}

	timeout := step.WaitTimeout
	if timeout == 0 {
		timeout = 10 * time.Minute // Default timeout
	}

	pollInterval := 15 * time.Second    // Poll every 15 seconds
	progressInterval := 1 * time.Second // Report progress every second
	startTime := time.Now()
	deadline := startTime.Add(timeout)
	lastProgressReport := startTime

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

	for {
		// Check if we've exceeded the timeout
		if time.Now().After(deadline) {
			return fmt.Errorf("timeout waiting for CI checks on PR #%d (%s) after %v", prNumber, step.BranchName, timeout)
		}

		// Report progress periodically
		now := time.Now()
		status, err := githubClient.GetPRChecksStatus(ctx.Context, step.BranchName)
		if err == nil && status != nil && opts.Handler != nil && now.Sub(lastProgressReport) >= progressInterval {
			elapsed := now.Sub(startTime)
			opts.Handler.StepWaiting(stepIndex, elapsed, timeout, status.Checks)
			lastProgressReport = now
		}

		if err != nil {
			// Log error but continue polling (might be transient)
			splog.Debug("Error checking CI status: %v", err)
		} else if status != nil {
			if !status.Passing {
				// CI checks failed
				return fmt.Errorf("CI checks failed on PR #%d (%s)", prNumber, step.BranchName)
			}

			// If we expect checks but none have appeared yet, treat as pending
			isReallyPending := status.Pending
			if step.ExpectChecks && len(status.Checks) == 0 {
				isReallyPending = true
				if now.Sub(startTime) > 5*time.Second {
					splog.Debug("No checks found yet for PR #%d, still waiting...", prNumber)
				}
			}

			if !isReallyPending {
				// All checks passed and none are pending
				if opts.Handler != nil {
					var maxDuration time.Duration
					for _, check := range status.Checks {
						if !check.FinishedAt.IsZero() && !check.StartedAt.IsZero() {
							d := check.FinishedAt.Sub(check.StartedAt)
							if d > maxDuration {
								maxDuration = d
							}
						}
					}
					if maxDuration > 0 {
						opts.Handler.SetEstimatedDuration(maxDuration)
					}
				}

				return nil
			}
		}

		// Wait before next poll
		remaining := time.Until(deadline)
		if remaining < pollInterval {
			time.Sleep(remaining)
		} else {
			time.Sleep(pollInterval)
		}
	}
}
