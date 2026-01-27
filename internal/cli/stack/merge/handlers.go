package merge

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	mergeAction "stackit.dev/stackit/internal/actions/merge"
	sterrors "stackit.dev/stackit/internal/errors"
	"stackit.dev/stackit/internal/output"
	"stackit.dev/stackit/internal/tui"
	mergeComponent "stackit.dev/stackit/internal/tui/components/merge"
	"stackit.dev/stackit/internal/tui/style"
)

// NewMergeUI creates a runner and handler pair for merge operations.
// The runner manages terminal state; the handler processes events.
// Caller must defer runner.Cleanup() to restore terminal on exit.
func NewMergeUI(out output.Output, logger output.Logger) (*tui.Runner, mergeAction.EventHandler) {
	if tui.IsTTY() {
		model := mergeComponent.NewModel()
		runner := tui.NewRunner(model, out, logger)
		runner.Start()
		return runner, NewInteractiveMergeEventHandler(runner, model)
	}
	return nil, NewSimpleMergeEventHandler(out)
}

// SimpleMergeEventHandler provides plain text output for merge operations
type SimpleMergeEventHandler struct {
	out output.Output
	mu  sync.Mutex
}

// NewSimpleMergeEventHandler creates a new SimpleMergeEventHandler
func NewSimpleMergeEventHandler(out output.Output) *SimpleMergeEventHandler {
	return &SimpleMergeEventHandler{out: out}
}

// Start implements EventHandler.
func (h *SimpleMergeEventHandler) Start(_ *mergeAction.Plan) {}

// EmitEvent implements EventHandler.
func (h *SimpleMergeEventHandler) EmitEvent(event mergeAction.Event) {
	h.mu.Lock()
	defer h.mu.Unlock()

	switch event.Type {
	case mergeAction.EventStarted:
		if event.Message != "" {
			h.out.Info("Starting: %s", event.Message)
		}
	case mergeAction.EventFailed:
		if event.Error != nil {
			h.out.Error("Step failed: %v", event.Error)
		}
	case mergeAction.EventWaiting:
		// Only report every 30 seconds to avoid spam
		if int(event.Elapsed.Seconds())%30 == 0 {
			h.out.Info("  ... still waiting (%v elapsed)", event.Elapsed.Round(time.Second))
		}
	case mergeAction.EventProgress:
		// Estimated duration updates are not shown in simple mode
	case mergeAction.EventCompleted:
		// Simple completion, not shown
	}
}

// Complete implements EventHandler.
func (h *SimpleMergeEventHandler) Complete(result *mergeAction.Result) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if result != nil && result.ConsolidationResult != nil {
		h.out.Info("✅ Created consolidation PR #%d: %s",
			result.ConsolidationResult.PRNumber,
			result.ConsolidationResult.PRURL)
	}
}

// Cleanup implements EventHandler. No-op for non-TTY handler.
func (h *SimpleMergeEventHandler) Cleanup() {}

// IsInteractive implements InteractiveHandler. Returns false for non-TTY handler.
func (h *SimpleMergeEventHandler) IsInteractive() bool {
	return false
}

// PromptMergeType implements InteractiveHandler. Returns error in non-interactive mode.
func (h *SimpleMergeEventHandler) PromptMergeType(_ bool, _ []string, _ []mergeAction.MultiStackInfo) (mergeAction.MergeType, error) {
	return "", fmt.Errorf("interactive mode required for merge type selection")
}

// PromptScope implements InteractiveHandler. Returns error in non-interactive mode.
func (h *SimpleMergeEventHandler) PromptScope(_ []string) (string, error) {
	return "", fmt.Errorf("interactive mode required for scope selection")
}

// PromptStacks implements InteractiveHandler. Returns error in non-interactive mode.
func (h *SimpleMergeEventHandler) PromptStacks(_ []mergeAction.MultiStackInfo) ([]string, error) {
	return nil, fmt.Errorf("interactive mode required for stack selection")
}

// PromptStrategy implements InteractiveHandler. Returns error in non-interactive mode.
func (h *SimpleMergeEventHandler) PromptStrategy(_ *mergeAction.Plan, _ mergeAction.Strategy) (mergeAction.StrategyChoice, error) {
	return mergeAction.StrategyChoice{}, fmt.Errorf("interactive mode required for strategy selection")
}

// PromptConfirm implements InteractiveHandler. Returns error in non-interactive mode.
func (h *SimpleMergeEventHandler) PromptConfirm(_ string, _ bool) (bool, error) {
	return false, fmt.Errorf("interactive mode required for confirmation")
}

// ShowPlan implements InteractiveHandler. Displays plan as text output.
func (h *SimpleMergeEventHandler) ShowPlan(plan *mergeAction.Plan, validation *mergeAction.PlanValidation) {
	planText := mergeAction.FormatMergePlan(plan, validation)
	h.out.Print(planText)
}

// ShowMidStackWarning implements InteractiveHandler.
func (h *SimpleMergeEventHandler) ShowMidStackWarning(scope string, upstackBranchesInScope []string) {
	h.out.Warn("⚠️  You're mid-stack in scope [%s]", scope)
	h.out.Warn("   Branches above in the same scope won't be merged: %s", strings.Join(upstackBranchesInScope, ", "))
	h.out.Info("   💡 To merge the entire scope, use: stackit merge --scope=%s", scope)
}

// PromptPostMerge implements InteractiveHandler. Returns Done in non-interactive mode.
func (h *SimpleMergeEventHandler) PromptPostMerge(_ bool, _ string) (mergeAction.PostMergeAction, error) {
	return mergeAction.PostMergeDone, nil
}

// PromptIndividualMerge implements InteractiveHandler. Returns error in non-interactive mode.
func (h *SimpleMergeEventHandler) PromptIndividualMerge(_ []mergeAction.BranchMergeInfo) (bool, error) {
	return false, fmt.Errorf("interactive mode required for individual merge selection")
}

// PromptSimpleMergeConfirm implements InteractiveHandler. Returns error in non-interactive mode.
func (h *SimpleMergeEventHandler) PromptSimpleMergeConfirm(_ mergeAction.BranchMergeInfo, _ string) (bool, error) {
	return false, fmt.Errorf("interactive mode required for confirmation")
}

// InteractiveMergeEventHandler provides a TUI for merge operations using runner.Send()
type InteractiveMergeEventHandler struct {
	runner *tui.Runner
	model  *mergeComponent.Model
	mu     sync.Mutex
	plan   *mergeAction.Plan
}

// NewInteractiveMergeEventHandler creates a new InteractiveMergeEventHandler
func NewInteractiveMergeEventHandler(runner *tui.Runner, model *mergeComponent.Model) *InteractiveMergeEventHandler {
	return &InteractiveMergeEventHandler{
		runner: runner,
		model:  model,
	}
}

// Start implements EventHandler.
func (h *InteractiveMergeEventHandler) Start(plan *mergeAction.Plan) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.plan = plan

	// Calculate groups for the TUI
	groups := CalculateGroups(plan)
	stepDescriptions := make([]string, len(plan.Steps))
	for i, step := range plan.Steps {
		stepDescriptions[i] = step.Description
	}

	// Convert to TUI component groups
	componentGroups := make([]mergeComponent.Group, len(groups))
	for i, g := range groups {
		componentGroups[i] = mergeComponent.Group{
			Label:       g.Label,
			StepIndices: g.StepIndices,
		}
	}

	// Send plan to the TUI
	h.runner.Send(mergeComponent.PlanLoadedMsg{
		Groups:           componentGroups,
		StepDescriptions: stepDescriptions,
	})
}

// EmitEvent implements EventHandler.
func (h *InteractiveMergeEventHandler) EmitEvent(event mergeAction.Event) {
	h.mu.Lock()
	defer h.mu.Unlock()

	switch event.Type {
	case mergeAction.EventStarted:
		h.runner.Send(mergeComponent.StepStartMsg{
			StepIndex:   event.StepIndex,
			Description: event.Message,
		})
	case mergeAction.EventCompleted:
		h.runner.Send(mergeComponent.StepCompleteMsg{
			StepIndex: event.StepIndex,
		})
	case mergeAction.EventFailed:
		h.runner.Send(mergeComponent.StepFailedMsg{
			StepIndex: event.StepIndex,
			Error:     event.Error,
		})
	case mergeAction.EventWaiting:
		h.runner.Send(mergeComponent.StepWaitingMsg{
			StepIndex: event.StepIndex,
			Elapsed:   event.Elapsed,
			Timeout:   event.Timeout,
			Checks:    event.Checks,
		})
	case mergeAction.EventProgress:
		if event.EstimatedDuration > 0 {
			h.runner.Send(mergeComponent.EstimatedDurationMsg(event.EstimatedDuration))
		}
	}
}

// Complete implements EventHandler.
func (h *InteractiveMergeEventHandler) Complete(result *mergeAction.Result) {
	h.mu.Lock()
	defer h.mu.Unlock()

	summary := ""
	if result != nil && result.ConsolidationResult != nil {
		summary = fmt.Sprintf("✅ Created consolidation PR #%d: %s",
			result.ConsolidationResult.PRNumber,
			result.ConsolidationResult.PRURL)
	}

	h.runner.Send(mergeComponent.CompleteMsg{Summary: summary})
}

// Cleanup implements EventHandler. No-op since runner cleanup is handled by defer.
func (h *InteractiveMergeEventHandler) Cleanup() {}

// IsInteractive implements InteractiveHandler. Returns true for TTY handler.
func (h *InteractiveMergeEventHandler) IsInteractive() bool {
	return true
}

// PromptMergeType implements InteractiveHandler.
func (h *InteractiveMergeEventHandler) PromptMergeType(canMergeThisBranch bool, availableScopes []string, availableStacks []mergeAction.MultiStackInfo) (mergeAction.MergeType, error) {
	h.runner.Pause()
	defer h.runner.Resume()

	var options []tui.SelectOption

	// Only show "This branch" if we're on a non-trunk branch
	if canMergeThisBranch {
		options = append(options, tui.SelectOption{
			Label: "🌿 This branch — Merge the current branch and its stack", Value: "this",
		})
	}
	if len(availableScopes) > 0 {
		options = append(options, tui.SelectOption{
			Label: "🏷️ Select a scope — Merge all branches in a specific scope", Value: "scope",
		})
	}
	if len(availableStacks) > 0 {
		options = append(options, tui.SelectOption{
			Label: "📚 Select stack(s) — Merge one or more stacks (multi-select)", Value: "stack",
		})
	}

	// If no options available, return an error
	if len(options) == 0 {
		return "", fmt.Errorf("no merge options available: no stacks or scopes found")
	}

	selected, err := tui.PromptSelect("What would you like to merge?", options, 0)
	if err != nil {
		return "", err
	}

	switch selected {
	case "this":
		return mergeAction.MergeTypeThis, nil
	case "scope":
		return mergeAction.MergeTypeScope, nil
	case "stack":
		return mergeAction.MergeTypeStacks, nil
	default:
		return mergeAction.MergeTypeThis, nil
	}
}

// PromptScope implements InteractiveHandler.
func (h *InteractiveMergeEventHandler) PromptScope(availableScopes []string) (string, error) {
	h.runner.Pause()
	defer h.runner.Resume()

	if len(availableScopes) == 0 {
		return "", fmt.Errorf("no branches with scopes found")
	}

	options := make([]tui.SelectOption, len(availableScopes))
	for i, s := range availableScopes {
		options[i] = tui.SelectOption{Label: s, Value: s}
	}

	return tui.PromptSelect("Select scope to merge:", options, 0)
}

// PromptStacks implements InteractiveHandler.
func (h *InteractiveMergeEventHandler) PromptStacks(availableStacks []mergeAction.MultiStackInfo) ([]string, error) {
	h.runner.Pause()
	defer h.runner.Resume()

	if len(availableStacks) == 0 {
		return nil, fmt.Errorf("no stacks found rooted at trunk")
	}

	// If only one stack, return it directly
	if len(availableStacks) == 1 {
		return []string{availableStacks[0].RootBranch}, nil
	}

	// Track selected stacks in order
	var selectedStacks []string
	selectedSet := make(map[string]bool)

	// Loop-based multi-select
	for {
		var options []tui.SelectOption

		// Add "Done" option if at least one stack is selected
		if len(selectedStacks) > 0 {
			doneLabel := fmt.Sprintf("✓ Done — Merge %d selected stack(s)", len(selectedStacks))
			options = append(options, tui.SelectOption{Label: doneLabel, Value: "done"})
		}

		// Add unselected stacks
		for _, stack := range availableStacks {
			if !selectedSet[stack.RootBranch] {
				label := "  " + mergeAction.FormatStackLabel(stack)
				options = append(options, tui.SelectOption{Label: label, Value: stack.RootBranch})
			}
		}

		// If all stacks selected, only show Done
		if len(options) == 1 && len(selectedStacks) > 0 {
			break
		}

		selected, err := tui.PromptSelect("Select a stack (or Done to proceed):", options, 0)
		if err != nil {
			// User canceled - always propagate the error, even if some stacks were selected
			return nil, err
		}

		if selected == "done" {
			break
		}

		// Add the selected stack
		selectedStacks = append(selectedStacks, selected)
		selectedSet[selected] = true
	}

	return selectedStacks, nil
}

// PromptStrategy implements InteractiveHandler.
func (h *InteractiveMergeEventHandler) PromptStrategy(plan *mergeAction.Plan, recommended mergeAction.Strategy) (mergeAction.StrategyChoice, error) {
	h.runner.Pause()
	defer h.runner.Resume()

	var strategyOptions []tui.SelectOption
	var defaultIndex int

	// Build options based on recommended strategy
	if recommended == mergeAction.StrategySquash {
		strategyOptions = []tui.SelectOption{
			{Label: "🔀 Squash — Create single PR with all stack commits for atomic merge (recommended)", Value: "squash"},
			{Label: "🔄 Bottom-up — Merge PRs one at a time from bottom", Value: "bottom-up"},
		}
		defaultIndex = 0
	} else {
		strategyOptions = []tui.SelectOption{
			{Label: "🔄 Bottom-up — Merge PRs one at a time from bottom (recommended)", Value: "bottom-up"},
			{Label: "🔀 Squash — Create single PR with all stack commits for atomic merge", Value: "squash"},
		}
		defaultIndex = 0
	}

	// Build prompt with branch count for context
	prompt := "Select merge strategy:"
	if plan != nil && len(plan.BranchesToMerge) > 0 {
		prompt = fmt.Sprintf("Select merge strategy for %d branches:", len(plan.BranchesToMerge))
	}

	selectedStrategy, err := tui.PromptSelect(prompt, strategyOptions, defaultIndex)
	if err != nil {
		return mergeAction.StrategyChoice{}, fmt.Errorf("strategy selection canceled: %w", err)
	}

	var strategy mergeAction.Strategy
	var wait bool

	switch selectedStrategy {
	case "bottom-up":
		strategy = mergeAction.StrategyBottomUp
	case "squash":
		strategy = mergeAction.StrategySquash
		// Prompt for wait if squashing (default is fire-and-forget)
		wait, err = tui.PromptConfirm("Wait for merge to complete? (default: fire-and-forget)", false)
		if err != nil {
			return mergeAction.StrategyChoice{}, fmt.Errorf("wait selection canceled: %w", err)
		}
	}

	return mergeAction.StrategyChoice{Strategy: strategy, Wait: wait}, nil
}

// PromptConfirm implements InteractiveHandler.
func (h *InteractiveMergeEventHandler) PromptConfirm(message string, defaultYes bool) (bool, error) {
	h.runner.Pause()
	defer h.runner.Resume()

	return tui.PromptConfirm(message, defaultYes)
}

// ShowPlan implements InteractiveHandler.
func (h *InteractiveMergeEventHandler) ShowPlan(plan *mergeAction.Plan, validation *mergeAction.PlanValidation) {
	h.runner.Pause()
	defer h.runner.Resume()

	// Use the plan formatting from the merge package
	planText := mergeAction.FormatMergePlan(plan, validation)
	fmt.Print(planText)
}

// ShowMidStackWarning implements InteractiveHandler.
func (h *InteractiveMergeEventHandler) ShowMidStackWarning(scope string, upstackBranchesInScope []string) {
	h.runner.Pause()
	defer h.runner.Resume()

	fmt.Printf("⚠️  You're mid-stack in scope [%s]\n", scope)
	fmt.Printf("   Branches above in the same scope won't be merged: %s\n", strings.Join(upstackBranchesInScope, ", "))
	fmt.Printf("   💡 To merge the entire scope, use: stackit merge --scope=%s\n\n", scope)
}

// PromptPostMerge implements InteractiveHandler.
func (h *InteractiveMergeEventHandler) PromptPostMerge(hasUncommittedChanges bool, trunkName string) (mergeAction.PostMergeAction, error) {
	h.runner.Pause()
	defer h.runner.Resume()

	fmt.Println()
	fmt.Println("🎉 Merge completed successfully in the temporary worktree!")
	fmt.Println("Your main workspace remains untouched.")

	trunkLabel := fmt.Sprintf("🔄 Switch to trunk and sync (%s)", trunkName)
	if hasUncommittedChanges {
		trunkLabel += " " + style.ColorYellow("(warning: you have local changes)")
	}

	options := []tui.SelectOption{
		{Label: trunkLabel, Value: "trunk-sync"},
		{Label: "Done", Value: "done"},
	}

	selected, err := tui.PromptSelect("What would you like to do in your main workspace?", options, 0)
	if err != nil {
		if errors.Is(err, sterrors.ErrCanceled) {
			return mergeAction.PostMergeDone, nil
		}
		return mergeAction.PostMergeDone, err
	}

	switch selected {
	case "trunk-sync":
		return mergeAction.PostMergeSyncTrunk, nil
	default:
		return mergeAction.PostMergeDone, nil
	}
}

// PromptIndividualMerge implements InteractiveHandler.
func (h *InteractiveMergeEventHandler) PromptIndividualMerge(branches []mergeAction.BranchMergeInfo) (bool, error) {
	h.runner.Pause()
	defer h.runner.Resume()

	// Build descriptive prompt
	branchCount := len(branches)
	var branchList strings.Builder
	for i, b := range branches {
		if i > 0 {
			branchList.WriteString(", ")
		}
		branchList.WriteString(fmt.Sprintf("PR #%d (%s)", b.PRNumber, b.BranchName))
		if i >= 2 && branchCount > 3 {
			fmt.Fprintf(&branchList, ", and %d more", branchCount-i-1)
			break
		}
	}

	fmt.Println()
	fmt.Printf("All %d PRs are independent branches with no conflicts.\n", branchCount)
	fmt.Printf("Branches: %s\n\n", branchList.String())

	options := []tui.SelectOption{
		{
			Label: "🔄 Merge individually — Merge each PR one at a time (recommended)",
			Value: "individual",
		},
		{
			Label: "🔀 Create consolidated PR — Combine all into a single merge commit",
			Value: "consolidate",
		},
	}

	selected, err := tui.PromptSelect("How would you like to merge these PRs?", options, 0)
	if err != nil {
		return false, err
	}

	return selected == "individual", nil
}

// PromptSimpleMergeConfirm implements InteractiveHandler.
func (h *InteractiveMergeEventHandler) PromptSimpleMergeConfirm(branch mergeAction.BranchMergeInfo, baseBranch string) (bool, error) {
	h.runner.Pause()
	defer h.runner.Resume()

	fmt.Println()
	fmt.Printf("Ready to merge PR #%d: %s\n", branch.PRNumber, branch.BranchName)
	fmt.Printf("Target: %s\n\n", baseBranch)

	return tui.PromptConfirm("Proceed?", false)
}

// Group represents a group of steps that should be displayed as a single line.
// Exported for use by the TUI component.
type Group struct {
	Label       string
	StepIndices []int
}

// CalculateGroups calculates groups for the TUI
func CalculateGroups(plan *mergeAction.Plan) []Group {
	// Pre-allocate a reasonable capacity for groups
	groups := make([]Group, 0, len(plan.BranchesToMerge)+len(plan.UpstackBranches)+4)
	assigned := make(map[int]bool)

	if plan.Strategy == mergeAction.StrategySquash {
		// For squash strategy: group by step type, not by individual branch
		groups = appendSquashGroups(groups, plan, assigned)
	} else {
		// For bottom-up strategy: group by individual PR
		groups = appendBottomUpGroups(groups, plan, assigned)
	}

	// Remaining steps (like PullTrunk) - applies to both strategies
	for i := range len(plan.Steps) {
		if assigned[i] {
			continue
		}

		label := plan.Steps[i].Description
		if plan.Steps[i].StepType == mergeAction.StepPullTrunk {
			label = "Sync trunk"
		}

		groups = append(groups, Group{
			Label:       label,
			StepIndices: []int{i},
		})
		assigned[i] = true
	}

	return groups
}

// appendSquashGroups appends groups for squash strategy
func appendSquashGroups(groups []Group, plan *mergeAction.Plan, assigned map[int]bool) []Group {
	// 1. Consolidation step
	var consolidationIndices []int
	for i, step := range plan.Steps {
		if step.StepType == mergeAction.StepConsolidate {
			consolidationIndices = append(consolidationIndices, i)
			assigned[i] = true
		}
	}
	if len(consolidationIndices) > 0 {
		groups = append(groups, Group{
			Label:       "Consolidate branches into single PR and wait for merge",
			StepIndices: consolidationIndices,
		})
	}

	// 2. All delete steps grouped together as "Cleanup merged branches"
	var deleteIndices []int
	for i, step := range plan.Steps {
		if step.StepType == mergeAction.StepDeleteBranch && !assigned[i] {
			deleteIndices = append(deleteIndices, i)
			assigned[i] = true
		}
	}
	if len(deleteIndices) > 0 {
		groups = append(groups, Group{
			Label:       "Cleanup merged branches",
			StepIndices: deleteIndices,
		})
	}

	// 3. All restack steps grouped together
	var restackIndices []int
	for i, step := range plan.Steps {
		if step.StepType == mergeAction.StepRestack && !assigned[i] {
			restackIndices = append(restackIndices, i)
			assigned[i] = true
		}
	}
	if len(restackIndices) > 0 {
		groups = append(groups, Group{
			Label:       "Restack upstack branches",
			StepIndices: restackIndices,
		})
	}

	return groups
}

// appendBottomUpGroups appends groups for bottom-up strategy
func appendBottomUpGroups(groups []Group, plan *mergeAction.Plan, assigned map[int]bool) []Group {
	// 1. Create groups for each branch being merged
	for _, branchInfo := range plan.BranchesToMerge {
		var indices []int
		for i, step := range plan.Steps {
			if step.BranchName == branchInfo.BranchName {
				indices = append(indices, i)
				assigned[i] = true
			}
		}
		if len(indices) > 0 {
			groups = append(groups, Group{
				Label:       fmt.Sprintf("PR #%d (%s)", branchInfo.PRNumber, branchInfo.BranchName),
				StepIndices: indices,
			})
		}
	}

	// 2. Create group for upstack branches
	if len(plan.UpstackBranches) > 0 {
		var indices []int
		for i, step := range plan.Steps {
			if assigned[i] {
				continue
			}
			for _, ub := range plan.UpstackBranches {
				if step.BranchName == ub {
					indices = append(indices, i)
					assigned[i] = true
					break
				}
			}
		}
		if len(indices) > 0 {
			groups = append(groups, Group{
				Label:       "Restack upstack branches",
				StepIndices: indices,
			})
		}
	}

	return groups
}
