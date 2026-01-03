package merge

import (
	"fmt"
	"time"

	"stackit.dev/stackit/internal/app"
	"stackit.dev/stackit/internal/config"
	"stackit.dev/stackit/internal/engine"
	"stackit.dev/stackit/internal/git"
	"stackit.dev/stackit/internal/github"
)

const (
	prStateOpen = "OPEN"
)

// mergeExecuteEngine is a minimal interface needed for executing a merge plan
type mergeExecuteEngine interface {
	engine.PRManager
	engine.BranchReader
	engine.BranchWriter
	engine.SyncManager
	engine.SplitManager
	engine.RemoteMetadataManager
	Git() git.Runner
}

// NullHandler is a no-op handler for testing or when output is not needed
type NullHandler struct{}

// Start implements Handler.
func (h *NullHandler) Start(_ *Plan) {}

// StepStarted implements Handler.
func (h *NullHandler) StepStarted(_ int, _ string) {}

// StepCompleted implements Handler.
func (h *NullHandler) StepCompleted(_ int) {}

// StepFailed implements Handler.
func (h *NullHandler) StepFailed(_ int, _ error) {}

// StepWaiting implements Handler.
func (h *NullHandler) StepWaiting(_ int, _, _ time.Duration, _ []github.CheckDetail) {}

// SetEstimatedDuration implements Handler.
func (h *NullHandler) SetEstimatedDuration(_ time.Duration) {}

// Complete implements Handler.
func (h *NullHandler) Complete(_ *ConsolidationResult) {}

// ExecuteOptions contains options for executing a merge plan
type ExecuteOptions struct {
	Plan                    *Plan
	Strategy                Strategy
	Force                   bool
	Wait                    bool                       // Whether to wait for CI/merge (applies to consolidate)
	Handler                 Handler                    // Optional progress handler
	UndoStackDepth          int                        // Maximum undo stack depth (from config)
	ConsolidationResultFunc func(*ConsolidationResult) // Callback for consolidation results
}

// Execute executes a validated merge plan step by step
func Execute(ctx *app.Context, eng mergeExecuteEngine, opts ExecuteOptions) error {
	plan := opts.Plan
	githubClient := ctx.GitHubClient
	splog := ctx.Splog

	// Use null handler if none provided
	if opts.Handler == nil {
		opts.Handler = &NullHandler{}
	}

	opts.Handler.Start(plan)

	// Calculate initial estimate if possible
	if githubClient != nil {
		initialEstimate := calculateBaselineEstimate(ctx.Context, plan, githubClient, splog)
		if initialEstimate > 0 {
			opts.Handler.SetEstimatedDuration(initialEstimate)
		}
	}

	// Set up callback to collect consolidation results if not already set
	var consolidationResult *ConsolidationResult
	if opts.ConsolidationResultFunc == nil {
		opts.ConsolidationResultFunc = func(result *ConsolidationResult) {
			consolidationResult = result
		}
	}

	// Execute plan (this will send updates to the handler)
	err := executeSteps(ctx, eng, opts)

	// Always call Complete to allow handlers to clean up (TUI, etc.)
	// consolidationResult will be nil if it wasn't reached or failed
	opts.Handler.Complete(consolidationResult)

	return err
}

// executeSteps executes the merge plan steps
func executeSteps(ctx *app.Context, eng mergeExecuteEngine, opts ExecuteOptions) error {
	plan := opts.Plan

	for i, step := range plan.Steps {
		// Report step started
		opts.Handler.StepStarted(i, step.Description)

		// 1. Re-validate preconditions for this step
		if err := validateStepPreconditions(ctx.Context, step, eng, ctx.GitHubClient, opts); err != nil {
			opts.Handler.StepFailed(i, err)
			return fmt.Errorf("step %d (%s) failed precondition: %w", i+1, step.Description, err)
		}

		// 2. Execute the step (with progress reporting for wait steps)
		if err := executeStepWithProgress(ctx, step, i, eng, opts); err != nil {
			ctx.Splog.Debug("Step %d (%s) failed: %v", i+1, step.Description, err)
			opts.Handler.StepFailed(i, err)
			return fmt.Errorf("step %d (%s) failed: %w", i+1, step.Description, err)
		}

		// 3. Report step completed
		opts.Handler.StepCompleted(i)
	}

	return nil
}

// executeStepWithProgress executes a step with progress reporting
func executeStepWithProgress(ctx *app.Context, step PlanStep, stepIndex int, eng mergeExecuteEngine, opts ExecuteOptions) error {
	// Special handling for wait steps to report progress
	if step.StepType == StepWaitCI {
		return executeWaitCIWithProgress(ctx, step, stepIndex, eng, opts)
	}
	return executeStep(ctx, step, eng, opts)
}

// executeStep executes a single step
func executeStep(ctx *app.Context, step PlanStep, eng mergeExecuteEngine, opts ExecuteOptions) error {
	trunk := eng.Trunk() // Cache trunk for this function scope
	trunkName := trunk.GetName()
	githubClient := ctx.GitHubClient
	splog := ctx.Splog
	repoRoot := ctx.RepoRoot

	switch step.StepType {
	case StepMergePR:
		if githubClient == nil {
			return fmt.Errorf("GitHub client not available")
		}
		splog.Debug("Executing StepMergePR for branch %s", step.BranchName)
		if err := githubClient.MergePullRequest(ctx.Context, step.BranchName); err != nil {
			splog.Debug("StepMergePR for branch %s failed: %v", step.BranchName, err)
			return fmt.Errorf("failed to merge PR: %w", err)
		}

	case StepPullTrunk:
		splog.Debug("Executing StepPullTrunk")
		pullResult, err := eng.PullTrunk(ctx.Context)
		if err != nil {
			splog.Debug("StepPullTrunk failed: %v", err)
			return fmt.Errorf("failed to pull trunk: %w", err)
		}
		switch pullResult {
		case engine.PullDone:
			trunk := eng.Trunk()
			rev, _ := trunk.GetRevision()
			revShort := rev
			if len(rev) > 7 {
				revShort = rev[:7]
			}
			splog.Debug("Trunk fast-forwarded to %s", revShort)
		case engine.PullUnneeded:
			splog.Debug("Trunk is up to date")
		case engine.PullConflict:
			return fmt.Errorf("trunk could not be fast-forwarded (conflict)")
		}

	case StepRestack:
		// Restack the branch - RestackBranches will automatically handle reparenting
		// if the parent has been merged/deleted
		branch := eng.GetBranch(step.BranchName)
		splog.Debug("Executing StepRestack for branch %s", step.BranchName)
		batchResult, err := eng.RestackBranches(ctx.Context, []engine.Branch{branch})
		result := batchResult.Results[step.BranchName]
		if err != nil {
			splog.Debug("StepRestack for branch %s failed: %v", step.BranchName, err)
			return fmt.Errorf("failed to restack: %w", err)
		}

		// Get the actual parent after restacking (may have been reparented)
		// Use NewParent from result if reparented, otherwise get from engine
		actualParent := result.NewParent
		if actualParent == "" {
			branch := eng.GetBranch(step.BranchName)
			parent := branch.GetParent()
			if parent == nil {
				actualParent = trunkName
			} else {
				actualParent = parent.GetName()
			}
		}

		switch result.Result {
		case engine.RestackDone:
			// Success - now push the rebased branch and update PR base
			// Force push is required since we rebased
			if err := eng.PushBranch(ctx.Context, step.BranchName, eng.GetRemote(), git.PushOptions{
				Force:    true,
				NoVerify: true, // Internal restack usually shouldn't run hooks
			}); err != nil {
				return fmt.Errorf("failed to push rebased branch %s: %w", step.BranchName, err)
			}
			splog.Debug("Pushed rebased branch %s to remote", step.BranchName)

			// Update the PR's base branch to the actual parent (not always trunk)
			if err := updatePRBaseBranchFromContext(ctx.Context, githubClient, step.BranchName, actualParent); err != nil {
				return fmt.Errorf("failed to update PR base for %s: %w", step.BranchName, err)
			}
			splog.Debug("Updated PR base for %s to %s", step.BranchName, actualParent)

		case engine.RestackConflict:
			// Save continuation state
			currentBranch := eng.CurrentBranch()
			currentBranchName := ""
			if currentBranch != nil {
				currentBranchName = currentBranch.GetName()
			}
			continuation := &config.ContinuationState{
				RebasedBranchBase:     result.RebasedBranchBase,
				CurrentBranchOverride: currentBranchName,
			}
			if err := config.PersistContinuationState(repoRoot, continuation); err != nil {
				return fmt.Errorf("failed to persist continuation: %w", err)
			}
			return fmt.Errorf("hit conflict restacking %s", step.BranchName)
		case engine.RestackUnneeded:
			// Already up to date, but still need to ensure PR base is correct
			// Push in case local is ahead of remote
			if err := eng.PushBranch(ctx.Context, step.BranchName, eng.GetRemote(), git.PushOptions{
				Force:    true,
				NoVerify: true,
			}); err != nil {
				splog.Debug("Failed to push branch %s (may already be up to date): %v", step.BranchName, err)
			}
			// Update PR base to the actual parent (not always trunk)
			if err := updatePRBaseBranchFromContext(ctx.Context, githubClient, step.BranchName, actualParent); err != nil {
				splog.Debug("Failed to update PR base for %s: %v", step.BranchName, err)
			}
		}

	case StepDeleteBranch:
		// Only delete if branch is tracked
		branch := eng.GetBranch(step.BranchName)
		if branch.IsTracked() {
			if err := eng.DeleteBranch(ctx.Context, branch); err != nil {
				// Non-fatal - branch might already be deleted
				splog.Debug("Failed to delete branch %s (may already be deleted): %v", step.BranchName, err)
			}
		}

	case StepUpdatePRBase:
		// For top-down strategy: rebase branch onto trunk and update PR base
		splog.Debug("Executing StepUpdatePRBase for branch %s", step.BranchName)
		if err := executeUpdatePRBase(ctx, eng, step); err != nil {
			splog.Debug("StepUpdatePRBase for branch %s failed: %v", step.BranchName, err)
			return err
		}

	case StepConsolidate:
		// Execute stack consolidation
		splog.Debug("Executing StepConsolidate")
		result, err := executeConsolidation(ctx, eng, opts)
		if err != nil {
			splog.Debug("StepConsolidate failed: %v", err)
			return err
		}
		// Notify caller of consolidation result
		if opts.ConsolidationResultFunc != nil {
			opts.ConsolidationResultFunc(result)
		}

	case StepWaitCI:
		// StepWaitCI should be handled by executeStepWithProgress, not executeStep
		// This case should never be reached.
		panic("StepWaitCI should be handled by executeStepWithProgress")

	default:
		return fmt.Errorf("unknown step type: %s", step.StepType)
	}

	return nil
}
