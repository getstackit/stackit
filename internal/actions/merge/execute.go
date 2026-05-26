package merge

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/config"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/internal/github"
	"github.com/getstackit/stackit/internal/output"
	"github.com/getstackit/stackit/internal/tui"
)

const (
	prStateOpen = "OPEN"
)

// GetMergeMethod returns the merge method to use for PR merges.
// If not configured, it prompts the user to select one and saves it to config.
func GetMergeMethod(ctx *app.Context, githubClient github.Client) (github.MergeMethod, error) {
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
		// Non-interactive mode: use the first allowed option
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

// NullEventHandler is a no-op EventHandler for testing or when output is not needed
type NullEventHandler struct{}

// Start implements EventHandler.
func (h *NullEventHandler) Start(_ *Plan) {}

// EmitEvent implements EventHandler.
func (h *NullEventHandler) EmitEvent(_ Event) {}

// Complete implements EventHandler.
func (h *NullEventHandler) Complete(_ *Result) {}

// Cleanup implements EventHandler.
func (h *NullEventHandler) Cleanup() {}

// ExecuteOptions contains options for executing a merge plan
type ExecuteOptions struct {
	Plan                    *Plan
	Strategy                Strategy
	Force                   bool
	Wait                    bool                       // Whether to wait for CI/merge (applies to consolidate)
	Handler                 EventHandler               // Optional progress handler
	UndoStackDepth          int                        // Maximum undo stack depth (from config)
	ConsolidationResultFunc func(*ConsolidationResult) // Callback for consolidation results
	MergeMethod             github.MergeMethod         // Optional: override merge method (empty = auto-detect/prompt)
}

// Execute executes a validated merge plan step by step
func Execute(ctx *app.Context, eng mergeExecuteEngine, opts ExecuteOptions) error {
	plan := opts.Plan
	githubClient := ctx.GitHub()
	out := ctx.Output

	// Use null handler if none provided
	if opts.Handler == nil {
		opts.Handler = &NullEventHandler{}
	}

	opts.Handler.Start(plan)

	// Calculate initial estimate if possible
	if githubClient != nil {
		initialEstimate := calculateBaselineEstimate(ctx.Context, plan, githubClient, out)
		if initialEstimate > 0 {
			opts.Handler.EmitEvent(Event{
				Type:              EventProgress,
				EstimatedDuration: initialEstimate,
			})
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
	opts.Handler.Complete(&Result{
		Success:             consolidationResult != nil || err == nil,
		ConsolidationResult: consolidationResult,
		Error:               err,
	})

	return err
}

// executeSteps executes the merge plan steps
func executeSteps(ctx *app.Context, eng mergeExecuteEngine, opts ExecuteOptions) error {
	plan := opts.Plan

	// Snapshot the worktree list once for the whole merge plan. A multi-branch
	// merge with several StepDeleteBranch steps used to spawn `git worktree
	// list` per step via removeWorktreeForBranch; the snapshot covers every
	// step in this run. Safe to reuse: deleting a branch can only invalidate
	// its own entry, and we only ever read each entry once.
	worktrees, err := eng.Git().ListWorktrees(ctx.Context)
	if err != nil {
		ctx.Output.Debug("Failed to list worktrees for merge plan: %v", err)
		worktrees = git.WorktreeList{}
	}

	for i, step := range plan.Steps {
		stepRef := &plan.Steps[i]

		// Report step started
		opts.Handler.EmitEvent(Event{
			Phase:     phaseFromStep(stepRef),
			Type:      EventStarted,
			StepIndex: i,
			Step:      stepRef,
			Message:   step.Description,
		})

		// 1. Re-validate preconditions for this step
		if err := validateStepPreconditions(ctx.Context, step, eng, ctx.GitHub(), opts); err != nil {
			opts.Handler.EmitEvent(Event{
				Phase:     phaseFromStep(stepRef),
				Type:      EventFailed,
				StepIndex: i,
				Step:      stepRef,
				Error:     err,
			})
			return fmt.Errorf("step %d (%s) failed precondition: %w", i+1, step.Description, err)
		}

		// 2. Execute the step (with progress reporting for wait steps)
		if err := executeStepWithProgress(ctx, step, i, eng, opts, worktrees); err != nil {
			ctx.Output.Debug("Step %d (%s) failed: %v", i+1, step.Description, err)
			opts.Handler.EmitEvent(Event{
				Phase:     phaseFromStep(stepRef),
				Type:      EventFailed,
				StepIndex: i,
				Step:      stepRef,
				Error:     err,
			})
			return fmt.Errorf("step %d (%s) failed: %w", i+1, step.Description, err)
		}

		// 3. Report step completed
		opts.Handler.EmitEvent(Event{
			Phase:     phaseFromStep(stepRef),
			Type:      EventCompleted,
			StepIndex: i,
			Step:      stepRef,
		})
	}

	return nil
}

// executeStepWithProgress executes a step with progress reporting
func executeStepWithProgress(ctx *app.Context, step PlanStep, stepIndex int, eng mergeExecuteEngine, opts ExecuteOptions, worktrees git.WorktreeList) error {
	// Special handling for wait steps to report progress
	if step.StepType == StepWaitCI {
		return executeWaitCIWithProgress(ctx, step, stepIndex, eng, opts)
	}
	return executeStep(ctx, step, stepIndex, eng, opts, worktrees)
}

// executeStep executes a single step
func executeStep(ctx *app.Context, step PlanStep, stepIndex int, eng mergeExecuteEngine, opts ExecuteOptions, worktrees git.WorktreeList) error {
	trunk := eng.Trunk() // Cache trunk for this function scope
	githubClient := ctx.GitHub()
	out := ctx.Output
	repoRoot := ctx.RepoRoot

	switch step.StepType {
	case StepMergePR:
		if githubClient == nil {
			return fmt.Errorf("GitHub client not available")
		}
		out.Debug("Executing StepMergePR for branch %s", step.BranchName)

		// Get merge method: use override if provided, otherwise detect/prompt
		mergeMethod := opts.MergeMethod
		if mergeMethod == "" {
			var err error
			mergeMethod, err = getMergeMethodWithPause(ctx, githubClient, opts.Handler)
			if err != nil {
				out.Debug("Failed to get merge method: %v", err)
				return fmt.Errorf("failed to get merge method: %w", err)
			}
		}

		if err := githubClient.MergePullRequest(ctx.Context, step.BranchName, github.MergePROptions{Method: mergeMethod}); err != nil {
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
		batchResult, err := eng.RestackBranches(ctx.Context, engine.BranchesOf(branch))
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
			actualParent = branch.GetParentOrTrunk()
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
			// Check if branch is checked out in a worktree and remove it first
			// Git refuses to delete a branch that is checked out in any worktree
			if err := removeWorktreeForBranch(ctx.Context, step.BranchName, worktrees, eng, out); err != nil {
				out.Warn("Failed to remove worktree for branch %s: %v", step.BranchName, err)
				// Continue anyway - deletion might still work if worktree is gone
			}

			if err := eng.DeleteBranch(ctx.Context, branch); err != nil {
				// Surface as warning so cleanup doesn't silently lie about success.
				// Engine already swallows "branch not found" — any error here is a real failure
				// the user needs to know about (e.g., branch checked out in another worktree).
				out.Warn("Failed to delete branch %s: %v", step.BranchName, err)
			} else {
				// Drop the remote metadata ref so it doesn't linger on origin and surface as
				// a phantom conflict next time someone runs sync.
				if err := eng.Git().BatchDeleteRemoteMetadataRefs(ctx.Context, []string{step.BranchName}); err != nil {
					out.Debug("Failed to delete remote metadata ref for %s: %v", step.BranchName, err)
				}
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
		// StepWaitCI should be handled by executeStepWithProgress, not executeStep.
		// If we reach here, it's a programming error.
		return fmt.Errorf("internal error: StepWaitCI should be handled by executeStepWithProgress, not executeStep")

	default:
		return fmt.Errorf("unknown step type: %s", step.StepType)
	}

	return nil
}

// removeWorktreeForBranch removes any worktree that has the given branch checked out.
// This is necessary because git refuses to delete a branch that is checked out in any worktree.
// Returns nil if no worktree exists or if removal succeeds.
//
// worktrees is a snapshot taken by the caller; passing it avoids spawning
// `git worktree list` per merge step.
func removeWorktreeForBranch(ctx context.Context, branchName string, worktrees git.WorktreeList, eng mergeExecuteEngine, out output.Output) error {
	worktreePath := worktrees.PathForBranch(branchName)
	if worktreePath == "" {
		return nil // Branch not in any worktree
	}

	// Don't remove main worktree (resolve symlinks for comparison, e.g., /var vs /private/var on macOS)
	repoRoot := eng.Git().GetRepoRoot()
	resolvedWorktree, _ := filepath.EvalSymlinks(worktreePath)
	resolvedRoot, _ := filepath.EvalSymlinks(repoRoot)
	if resolvedWorktree == resolvedRoot {
		out.Debug("Branch %s is in main worktree, cannot remove", branchName)
		return fmt.Errorf("branch %s is checked out in main worktree", branchName)
	}

	out.Debug("Removing worktree at %s for branch %s", worktreePath, branchName)

	if err := eng.Git().RemoveWorktree(ctx, worktreePath); err != nil {
		return fmt.Errorf("failed to remove worktree at %s for branch %s: %w", worktreePath, branchName, err)
	}

	out.Info("Removed worktree at %s for branch %s", worktreePath, branchName)
	return nil
}
