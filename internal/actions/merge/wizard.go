package merge

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"stackit.dev/stackit/internal/app"
	"stackit.dev/stackit/internal/config"
	"stackit.dev/stackit/internal/engine"
	sterrors "stackit.dev/stackit/internal/errors"
	"stackit.dev/stackit/internal/github"
)

// WizardOptions configures the interactive merge wizard
type WizardOptions struct {
	DryRun       bool     // If true, show plan but don't execute
	Force        bool     // Skip validation checks
	Scope        string   // Pre-selected scope (empty = prompt or use current branch)
	TargetBranch string   // Pre-selected target branch (empty = current branch)
	Strategy     Strategy // Pre-selected strategy (empty = prompt)
	Wait         bool     // Pre-selected wait option (for consolidate)
}

// RunWizard executes the interactive merge wizard.
// It guides the user through selecting what to merge, the strategy,
// and then executes the merge.
func RunWizard(ctx *app.Context, handler InteractiveHandler, opts WizardOptions) error {
	eng := ctx.Engine
	out := ctx.Output

	out.Debug("merge wizard: starting with opts=%+v", opts)

	// Populate remote SHAs so we can accurately check if branches match remote
	if err := eng.PopulateRemoteShas(); err != nil {
		out.Debug("merge wizard: failed to populate remote SHAs: %v", err)
	}

	// Determine what to merge if not pre-selected
	scope := opts.Scope
	targetBranch := opts.TargetBranch

	if scope == "" && targetBranch == "" {
		out.Debug("merge wizard: no scope or target branch pre-selected, prompting user")

		// Check if "this branch" is a valid option (not on trunk and not in empty worktree)
		currentBranch := eng.CurrentBranch()
		canMergeThisBranch := currentBranch != nil && !currentBranch.IsTrunk()
		out.Debug("merge wizard: canMergeThisBranch=%v (currentBranch=%v)", canMergeThisBranch, currentBranch != nil)

		// Get available scopes and stacks for the prompt
		availableScopes := GetAvailableScopes(eng)
		availableStacks, err := DiscoverStacks(eng)
		if err != nil {
			out.Debug("merge wizard: failed to discover stacks: %v", err)
			availableStacks = nil
		}
		out.Debug("merge wizard: found %d scopes, %d stacks", len(availableScopes), len(availableStacks))

		mergeType, err := handler.PromptMergeType(canMergeThisBranch, availableScopes, availableStacks)
		if err != nil {
			out.Debug("merge wizard: merge type prompt error: %v", err)
			return err
		}
		out.Debug("merge wizard: user selected merge type: %s", mergeType)

		switch mergeType {
		case MergeTypeThis:
			// Use current branch
			currentBranch := eng.CurrentBranch()
			if currentBranch != nil {
				targetBranch = currentBranch.GetName()
				out.Debug("merge wizard: using current branch: %s", targetBranch)
			}
		case MergeTypeScope:
			scope, err = handler.PromptScope(availableScopes)
			if err != nil {
				out.Debug("merge wizard: scope prompt error: %v", err)
				return err
			}
			out.Debug("merge wizard: user selected scope: %s", scope)
		case MergeTypeStacks:
			selectedStacks, err := handler.PromptStacks(availableStacks)
			if err != nil {
				out.Debug("merge wizard: stack prompt error: %v", err)
				return err
			}
			out.Debug("merge wizard: user selected %d stacks: %v", len(selectedStacks), selectedStacks)

			if len(selectedStacks) == 0 {
				out.Debug("merge wizard: no stacks selected, exiting")
				out.Info("No stacks selected")
				return nil
			}
			if len(selectedStacks) == 1 {
				// Single stack: find the tip branch
				for _, stack := range availableStacks {
					if stack.RootBranch == selectedStacks[0] {
						targetBranch = stack.AllBranches[len(stack.AllBranches)-1]
						out.Debug("merge wizard: single stack selected, using tip branch: %s", targetBranch)
						break
					}
				}
			} else {
				// Multiple stacks: use multi-stack merge
				out.Debug("merge wizard: multiple stacks selected, delegating to ExecuteMultiStack")
				_, err := ExecuteMultiStack(ctx, MultiStackOptions{
					SelectedStacks: selectedStacks,
					Wait:           opts.Wait,
				})
				return err
			}
		}
	} else {
		out.Debug("merge wizard: using pre-selected scope=%q, targetBranch=%q", scope, targetBranch)
	}

	// Create initial plan with bottom-up strategy (will be refined)
	out.Debug("merge wizard: creating initial plan with scope=%q, targetBranch=%q", scope, targetBranch)
	plan, validation, err := CreateMergePlan(ctx.Context, eng, out, ctx.GitHubClient, CreatePlanOptions{
		Strategy:     StrategyBottomUp,
		Force:        opts.Force,
		Scope:        scope,
		TargetBranch: targetBranch,
		Wait:         false,
	})
	if err != nil {
		out.Debug("merge wizard: failed to create initial plan: %v", err)
		return err
	}
	out.Debug("merge wizard: initial plan created with %d branches to merge, %d steps", len(plan.BranchesToMerge), len(plan.Steps))

	// Check for mid-stack scope warning
	if scope == "" && len(plan.BranchesToMerge) > 0 {
		currentBranch := eng.GetBranch(plan.CurrentBranch)
		currentScope := eng.GetScope(currentBranch).String()
		if currentScope != "" {
			upstackInScope := AnalyzeMidStackScope(eng, plan, currentScope)
			if len(upstackInScope) > 0 {
				handler.ShowMidStackWarning(currentScope, upstackInScope)
			}
		}
	}

	// Show validation issues before strategy selection
	if len(validation.Errors) > 0 {
		out.Print("Validation Errors:\n")
		for _, e := range validation.Errors {
			out.Print(fmt.Sprintf("  ✗ %s\n", e))
		}
		out.Newline()
	}
	if len(validation.Warnings) > 0 {
		out.Print("Warnings:\n")
		for _, w := range validation.Warnings {
			out.Print(fmt.Sprintf("  ⚠ %s\n", w))
		}
		out.Newline()
	}

	// Check validation
	if !validation.Valid && !opts.Force {
		return formatValidationError(validation.Errors, validation.Warnings)
	}

	if len(validation.Warnings) > 0 && !opts.Force {
		return formatValidationError(nil, validation.Warnings)
	}

	// Determine strategy
	var strategy Strategy
	var wait bool

	switch {
	case opts.Strategy != "":
		strategy = opts.Strategy
		wait = opts.Wait
		out.Debug("merge wizard: using pre-selected strategy: %s, wait=%v", strategy, wait)
	case len(plan.BranchesToMerge) == 1:
		// Single PR: auto-select bottom-up
		strategy = StrategyBottomUp
		out.Debug("merge wizard: auto-selected bottom-up strategy for single PR")
	default:
		// Prompt for strategy
		recommended := DetermineRecommendedStrategy(len(plan.BranchesToMerge))
		out.Debug("merge wizard: prompting for strategy, recommended=%s", recommended)
		choice, err := handler.PromptStrategy(plan, recommended)
		if err != nil {
			out.Debug("merge wizard: strategy prompt error: %v", err)
			return err
		}
		strategy = choice.Strategy
		wait = choice.Wait
		out.Debug("merge wizard: user selected strategy=%s, wait=%v", strategy, wait)
	}

	// Recreate plan with selected strategy
	out.Debug("merge wizard: recreating plan with strategy=%s", strategy)
	plan, validation, err = CreateMergePlan(ctx.Context, eng, out, ctx.GitHubClient, CreatePlanOptions{
		Strategy:     strategy,
		Force:        opts.Force,
		Scope:        scope,
		TargetBranch: targetBranch,
		Wait:         wait,
	})
	if err != nil {
		out.Debug("merge wizard: failed to recreate plan: %v", err)
		return err
	}
	out.Debug("merge wizard: final plan has %d branches, %d steps", len(plan.BranchesToMerge), len(plan.Steps))

	// Streamlined UX for simple merges:
	// When merging a single leaf branch (no children, no upstack work) with no validation
	// issues, we skip the full plan display and use a simple "Merge X into Y?" confirmation.
	// This reduces friction for the common case of merging a single PR, while still showing
	// the full plan for complex scenarios where users need to understand all the steps.
	graph := engine.BuildStackGraph(eng, engine.SortStrategyAlphabetical, nil)
	isSimpleMerge := IsSingleBranchLeafMerge(plan, graph) &&
		len(validation.Errors) == 0 &&
		len(validation.Warnings) == 0

	if isSimpleMerge && !opts.DryRun {
		// Use simplified confirmation for single-branch merges
		branch := plan.BranchesToMerge[0]
		baseBranch := eng.Trunk().GetName()
		out.Debug("merge wizard: simple single-branch merge detected, using simplified confirmation")

		confirmed, err := handler.PromptSimpleMergeConfirm(branch, baseBranch)
		if err != nil {
			out.Debug("merge wizard: simple confirmation error: %v", err)
			return fmt.Errorf("confirmation canceled: %w", err)
		}
		if !confirmed {
			out.Debug("merge wizard: user declined simple confirmation")
			out.Info("Merge canceled")
			return nil
		}
		out.Debug("merge wizard: user confirmed simple merge")
	} else {
		// Show the full plan for complex merges or when there are warnings/errors
		handler.ShowPlan(plan, validation)

		// Re-validate with new strategy
		if !validation.Valid && !opts.Force {
			out.Debug("merge wizard: validation failed")
			return formatValidationError(validation.Errors, validation.Warnings)
		}

		// If dry-run, stop here
		if opts.DryRun {
			out.Debug("merge wizard: dry-run mode, stopping before execution")
			out.Info("Dry-run mode: plan displayed above. Use without --dry-run to execute.")
			return nil
		}

		// Confirm before executing
		confirmed, err := handler.PromptConfirm("Proceed with merge?", false)
		if err != nil {
			out.Debug("merge wizard: confirmation error: %v", err)
			return fmt.Errorf("confirmation canceled: %w", err)
		}
		if !confirmed {
			out.Debug("merge wizard: user declined confirmation")
			out.Info("Merge canceled")
			return nil
		}
		out.Debug("merge wizard: user confirmed, proceeding with merge")
	}

	// Get config values
	cfg, _ := config.LoadConfig(ctx.RepoRoot)
	undoStackDepth := cfg.UndoStackDepth()

	// Execute the merge
	mergeOpts := Options{
		DryRun:         opts.DryRun,
		Confirm:        false, // Already confirmed
		Strategy:       strategy,
		Force:          opts.Force,
		Wait:           wait,
		Scope:          scope,
		TargetBranch:   targetBranch,
		Plan:           plan,
		UndoStackDepth: undoStackDepth,
		Handler:        NewLegacyHandlerAdapter(handler),
	}

	out.Debug("merge wizard: executing merge action")
	if err := Action(ctx, mergeOpts); err != nil {
		out.Debug("merge wizard: merge action failed: %v", err)
		return fmt.Errorf("merge action failed: %w", err)
	}
	out.Debug("merge wizard: merge action completed successfully")

	// Handle post-merge follow-up
	out.Debug("merge wizard: handling post-merge")
	return HandlePostMerge(ctx, handler)
}

// DetermineRecommendedStrategy returns the recommended strategy based on stack size.
// - 1-2 branches: bottom-up (merge one at a time)
// - 3+ branches: squash (atomic merge)
func DetermineRecommendedStrategy(branchCount int) Strategy {
	switch {
	case branchCount <= 2:
		return StrategyBottomUp
	default:
		return StrategySquash
	}
}

// HandlePostMerge handles the post-merge follow-up workflow.
func HandlePostMerge(ctx *app.Context, handler InteractiveHandler) error {
	eng := ctx.Engine

	// Check for uncommitted changes
	hasChanges := eng.HasUncommittedChanges(ctx.Context)
	trunkName := eng.Trunk().GetName()

	action, err := handler.PromptPostMerge(hasChanges, trunkName)
	if err != nil {
		// User cancellation is expected - skip post-merge action
		if errors.Is(err, sterrors.ErrCanceled) {
			return nil
		}
		// Propagate real errors
		return err
	}

	switch action {
	case PostMergeSyncTrunk:
		// The handler will need to handle this action
		// This involves checking out trunk and running sync
		// For now, return a sentinel error that the CLI can handle
		return &PostMergeActionRequired{Action: action}
	case PostMergeDone:
		return nil
	}

	return nil
}

// PostMergeActionRequired is returned when post-merge action is needed
type PostMergeActionRequired struct {
	Action PostMergeAction
}

func (e *PostMergeActionRequired) Error() string {
	return fmt.Sprintf("post-merge action required: %s", e.Action)
}

// LegacyHandlerAdapter adapts EventHandler to the legacy Handler interface.
// This allows using the new event-based handlers with the existing execute.go code.
type LegacyHandlerAdapter struct {
	handler EventHandler
	plan    *Plan
}

// NewLegacyHandlerAdapter creates a new adapter wrapping an EventHandler.
func NewLegacyHandlerAdapter(handler EventHandler) *LegacyHandlerAdapter {
	return &LegacyHandlerAdapter{handler: handler}
}

// Start implements Handler.
func (a *LegacyHandlerAdapter) Start(plan *Plan) {
	a.plan = plan
	a.handler.Start(plan)
}

// StepStarted implements Handler.
func (a *LegacyHandlerAdapter) StepStarted(stepIndex int, description string) {
	var step *PlanStep
	if a.plan != nil && stepIndex < len(a.plan.Steps) {
		step = &a.plan.Steps[stepIndex]
	}
	a.handler.EmitEvent(Event{
		Phase:     phaseFromStep(step),
		Type:      EventStarted,
		StepIndex: stepIndex,
		Step:      step,
		Message:   description,
	})
}

// StepCompleted implements Handler.
func (a *LegacyHandlerAdapter) StepCompleted(stepIndex int) {
	var step *PlanStep
	if a.plan != nil && stepIndex < len(a.plan.Steps) {
		step = &a.plan.Steps[stepIndex]
	}
	a.handler.EmitEvent(Event{
		Phase:     phaseFromStep(step),
		Type:      EventCompleted,
		StepIndex: stepIndex,
		Step:      step,
	})
}

// StepFailed implements Handler.
func (a *LegacyHandlerAdapter) StepFailed(stepIndex int, err error) {
	var step *PlanStep
	if a.plan != nil && stepIndex < len(a.plan.Steps) {
		step = &a.plan.Steps[stepIndex]
	}
	a.handler.EmitEvent(Event{
		Phase:     phaseFromStep(step),
		Type:      EventFailed,
		StepIndex: stepIndex,
		Step:      step,
		Error:     err,
	})
}

// StepWaiting implements Handler.
func (a *LegacyHandlerAdapter) StepWaiting(stepIndex int, elapsed, timeout time.Duration, checks []github.CheckDetail) {
	var step *PlanStep
	if a.plan != nil && stepIndex < len(a.plan.Steps) {
		step = &a.plan.Steps[stepIndex]
	}
	a.handler.EmitEvent(Event{
		Phase:     PhaseWaiting,
		Type:      EventWaiting,
		StepIndex: stepIndex,
		Step:      step,
		Elapsed:   elapsed,
		Timeout:   timeout,
		Checks:    checks,
	})
}

// SetEstimatedDuration implements Handler.
func (a *LegacyHandlerAdapter) SetEstimatedDuration(duration time.Duration) {
	a.handler.EmitEvent(Event{
		Type:              EventProgress,
		EstimatedDuration: duration,
	})
}

// Complete implements Handler.
func (a *LegacyHandlerAdapter) Complete(result *ConsolidationResult) {
	a.handler.Complete(&Result{
		Success:             result != nil,
		ConsolidationResult: result,
	})
}

func phaseFromStep(step *PlanStep) Phase {
	if step == nil {
		return PhaseMerge
	}
	switch step.StepType {
	case StepMergePR:
		return PhaseMerge
	case StepRestack, StepUpdatePRBase:
		return PhaseRestack
	case StepDeleteBranch, StepPullTrunk:
		return PhaseCleanup
	case StepWaitCI:
		return PhaseWaiting
	case StepConsolidate:
		return PhaseMerge
	default:
		return PhaseMerge
	}
}

// formatValidationError creates a detailed error message including validation errors and warnings.
func formatValidationError(errors, warnings []string) error {
	var parts []string

	if len(errors) > 0 {
		parts = append(parts, "validation failed:")
		for _, e := range errors {
			parts = append(parts, fmt.Sprintf("  ✗ %s", e))
		}
	}

	if len(warnings) > 0 {
		if len(errors) == 0 {
			parts = append(parts, "merge blocked due to warnings:")
		}
		for _, w := range warnings {
			parts = append(parts, fmt.Sprintf("  ⚠ %s", w))
		}
	}

	parts = append(parts, "(use --force to override)")

	return fmt.Errorf("%s", strings.Join(parts, "\n"))
}
