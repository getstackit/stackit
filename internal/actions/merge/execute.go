package merge

import (
	"fmt"
	"time"

	"stackit.dev/stackit/internal/app"
	"stackit.dev/stackit/internal/config"
	"stackit.dev/stackit/internal/engine"
	"stackit.dev/stackit/internal/git"
	"stackit.dev/stackit/internal/github"
	"stackit.dev/stackit/internal/tui"
)

const (
	prStateOpen = "OPEN"
)

// getMergeMethod returns the merge method to use for PR merges.
// If not configured, it prompts the user to select one and saves it to config.
func getMergeMethod(ctx *app.Context, githubClient github.Client) (github.MergeMethod, error) {
	// Load config
	cfg, err := config.LoadConfig(ctx.RepoRoot)
	if err != nil {
		return "", fmt.Errorf("failed to load config: %w", err)
	}

	// Check if already configured
	if method := cfg.MergeMethod(); method != "" {
		return github.MergeMethod(method), nil
	}

	// Query allowed methods from GitHub
	settings, err := githubClient.GetAllowedMergeMethods(ctx.Context)
	if err != nil {
		return "", fmt.Errorf("failed to get allowed merge methods: %w", err)
	}

	// Build list of allowed methods
	var options []tui.SelectOption
	if settings.AllowSquashMerge {
		options = append(options, tui.SelectOption{
			Label: "squash (Squash and merge)",
			Value: "squash",
		})
	}
	if settings.AllowMergeCommit {
		options = append(options, tui.SelectOption{
			Label: "merge (Create a merge commit)",
			Value: "merge",
		})
	}
	if settings.AllowRebaseMerge {
		options = append(options, tui.SelectOption{
			Label: "rebase (Rebase and merge)",
			Value: "rebase",
		})
	}

	if len(options) == 0 {
		return "", fmt.Errorf("no merge methods are allowed for this repository")
	}

	// If only one option, use it automatically
	if len(options) == 1 {
		method := options[0].Value
		if err := cfg.SetMergeMethod(method); err != nil {
			return "", fmt.Errorf("failed to save merge method: %w", err)
		}
		if err := cfg.Save(); err != nil {
			return "", fmt.Errorf("failed to save config: %w", err)
		}
		ctx.Output.Info("Using merge method: %s (only option available)", method)
		return github.MergeMethod(method), nil
	}

	// Check if interactive mode is available
	if err := tui.CheckInteractiveAllowed(); err != nil {
		// Non-interactive mode: use the first option (squash if available)
		method := options[0].Value
		if err := cfg.SetMergeMethod(method); err != nil {
			return "", fmt.Errorf("failed to save merge method: %w", err)
		}
		if err := cfg.Save(); err != nil {
			return "", fmt.Errorf("failed to save config: %w", err)
		}
		ctx.Output.Info("Using merge method: %s (auto-selected in non-interactive mode)", method)
		return github.MergeMethod(method), nil
	}

	// Prompt user to select
	ctx.Output.Info("Select a merge method for this repository:")
	selected, err := tui.PromptSelect("Select merge method:", options, 0)
	if err != nil {
		return "", fmt.Errorf("failed to select merge method: %w", err)
	}

	// Save to config
	if err := cfg.SetMergeMethod(selected); err != nil {
		return "", fmt.Errorf("failed to save merge method: %w", err)
	}
	if err := cfg.Save(); err != nil {
		return "", fmt.Errorf("failed to save config: %w", err)
	}

	ctx.Output.Info("Saved merge.method = %s to config", selected)
	return github.MergeMethod(selected), nil
}

// mergeExecuteEngine is a minimal interface needed for executing a merge plan
type mergeExecuteEngine interface {
	engine.PRManager
	engine.BranchReader
	engine.BranchWriter
	engine.SyncManager
	engine.StackRewriter
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
	out := ctx.Output

	// Use null handler if none provided
	if opts.Handler == nil {
		opts.Handler = &NullHandler{}
	}

	opts.Handler.Start(plan)

	// Calculate initial estimate if possible
	if githubClient != nil {
		initialEstimate := calculateBaselineEstimate(ctx.Context, plan, githubClient, out)
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
			ctx.Output.Debug("Step %d (%s) failed: %v", i+1, step.Description, err)
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
	return executeStep(ctx, step, stepIndex, eng, opts)
}

// executeStep executes a single step
func executeStep(ctx *app.Context, step PlanStep, stepIndex int, eng mergeExecuteEngine, opts ExecuteOptions) error {
	trunk := eng.Trunk() // Cache trunk for this function scope
	trunkName := trunk.GetName()
	githubClient := ctx.GitHubClient
	out := ctx.Output
	repoRoot := ctx.RepoRoot

	switch step.StepType {
	case StepMergePR:
		if githubClient == nil {
			return fmt.Errorf("GitHub client not available")
		}
		out.Debug("Executing StepMergePR for branch %s", step.BranchName)

		// Get merge method (prompts user if not configured)
		mergeMethod, err := getMergeMethod(ctx, githubClient)
		if err != nil {
			out.Debug("Failed to get merge method: %v", err)
			return fmt.Errorf("failed to get merge method: %w", err)
		}

		if err := githubClient.MergePullRequest(ctx.Context, step.BranchName, mergeMethod); err != nil {
			out.Debug("StepMergePR for branch %s failed: %v", step.BranchName, err)
			return fmt.Errorf("failed to merge PR: %w", err)
		}

	case StepPullTrunk:
		out.Debug("Executing StepPullTrunk")
		pullResult, err := eng.PullTrunk(ctx.Context)
		if err != nil {
			out.Debug("StepPullTrunk failed: %v", err)
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
			out.Debug("Trunk fast-forwarded to %s", revShort)
		case engine.PullUnneeded:
			out.Debug("Trunk is up to date")
		case engine.PullConflict:
			return fmt.Errorf("trunk could not be fast-forwarded (conflict). This usually means your local trunk branch has diverged from the remote. Please sync your trunk branch manually")
		}

	case StepRestack:
		// Restack the branch - RestackBranches will automatically handle reparenting
		// if the parent has been merged/deleted
		branch := eng.GetBranch(step.BranchName)
		out.Debug("Executing StepRestack for branch %s", step.BranchName)
		batchResult, err := eng.RestackBranches(ctx.Context, []engine.Branch{branch})
		result := batchResult.Results[step.BranchName]
		if err != nil {
			out.Debug("StepRestack for branch %s failed: %v", step.BranchName, err)
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
			if err := eng.PushBranch(ctx.Context, eng.GetBranch(step.BranchName), eng.GetRemote(), git.PushOptions{
				Force:    true,
				NoVerify: true, // Internal restack usually shouldn't run hooks
			}); err != nil {
				return fmt.Errorf("failed to push rebased branch %s: %w", step.BranchName, err)
			}
			out.Debug("Pushed rebased branch %s to remote", step.BranchName)

			// Update the PR's base branch to the actual parent (not always trunk)
			if err := updatePRBaseBranchFromContext(ctx.Context, githubClient, step.BranchName, actualParent); err != nil {
				return fmt.Errorf("failed to update PR base for %s: %w", step.BranchName, err)
			}
			out.Debug("Updated PR base for %s to %s", step.BranchName, actualParent)

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
			if err := eng.PushBranch(ctx.Context, eng.GetBranch(step.BranchName), eng.GetRemote(), git.PushOptions{
				Force:    true,
				NoVerify: true,
			}); err != nil {
				out.Debug("Failed to push branch %s (may already be up to date): %v", step.BranchName, err)
			}
			// Update PR base to the actual parent (not always trunk)
			if err := updatePRBaseBranchFromContext(ctx.Context, githubClient, step.BranchName, actualParent); err != nil {
				out.Debug("Failed to update PR base for %s: %v", step.BranchName, err)
			}
		}

	case StepDeleteBranch:
		// Only delete if branch is tracked
		branch := eng.GetBranch(step.BranchName)
		if branch.IsTracked() {
			if err := eng.DeleteBranch(ctx.Context, branch); err != nil {
				// Non-fatal - branch might already be deleted
				out.Debug("Failed to delete branch %s (may already be deleted): %v", step.BranchName, err)
			}
		}

	case StepUpdatePRBase:
		// For top-down strategy: rebase branch onto trunk and update PR base
		out.Debug("Executing StepUpdatePRBase for branch %s", step.BranchName)
		if err := executeUpdatePRBase(ctx, eng, step); err != nil {
			out.Debug("StepUpdatePRBase for branch %s failed: %v", step.BranchName, err)
			return err
		}

	case StepConsolidate:
		// Execute stack consolidation
		out.Debug("Executing StepConsolidate")
		result, err := executeConsolidation(ctx, eng, stepIndex, opts)
		if err != nil {
			out.Debug("StepConsolidate failed: %v", err)
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
