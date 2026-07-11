package merge

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	mergeAction "github.com/getstackit/stackit/internal/actions/merge"
	sterrors "github.com/getstackit/stackit/internal/errors"
	"github.com/getstackit/stackit/internal/output"
	"github.com/getstackit/stackit/internal/shippable"
	"github.com/getstackit/stackit/internal/tui"
	mergeComponent "github.com/getstackit/stackit/internal/tui/components/merge"
	"github.com/getstackit/stackit/internal/tui/style"
)

// NewMergeUI creates a runner and handler pair for merge operations.
// The runner manages terminal state; the handler processes events.
// Caller must defer runner.Cleanup() to restore terminal on exit.
func NewMergeUI(out output.Output, logger output.Logger) (*tui.Runner, mergeAction.EventHandler) {
	if tui.IsTTY() {
		model := mergeComponent.NewModel()
		runner := tui.NewRunner(model, out, logger)
		runner.Start()
		return runner, NewInteractiveMergeEventHandler(runner, model, out)
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

// Pause implements optional pauser interface. No-op for non-TTY handler.
func (h *SimpleMergeEventHandler) Pause() {}

// Resume implements optional pauser interface. No-op for non-TTY handler.
func (h *SimpleMergeEventHandler) Resume() {}

// InteractiveMergeEventHandler provides a TUI for merge operations using runner.Send()
type InteractiveMergeEventHandler struct {
	runner *tui.Runner
	model  *mergeComponent.Model
	out    output.Output
	mu     sync.Mutex
	plan   *mergeAction.Plan
}

// NewInteractiveMergeEventHandler creates a new InteractiveMergeEventHandler
func NewInteractiveMergeEventHandler(runner *tui.Runner, model *mergeComponent.Model, out output.Output) *InteractiveMergeEventHandler {
	return &InteractiveMergeEventHandler{
		runner: runner,
		model:  model,
		out:    out,
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

// Pause implements optional pauser interface. Releases the terminal for prompts.
func (h *InteractiveMergeEventHandler) Pause() { h.runner.Pause() }

// Resume implements optional pauser interface. Restores the TUI after prompts.
func (h *InteractiveMergeEventHandler) Resume() { h.runner.Resume() }

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
			options = append(options, tui.SelectOption{Label: doneLabel, Value: mergeStrategyDone})
		}

		// Add unselected stacks
		for _, stack := range availableStacks {
			if !selectedSet[stack.RootBranch] {
				label := "  " + stack.Label()
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

		if selected == mergeStrategyDone {
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
	if recommended == mergeAction.StrategyShip {
		strategyOptions = []tui.SelectOption{
			{Label: "🔀 Ship — Create single PR with all stack commits for atomic merge (recommended)", Value: mergeStrategyShip},
			{Label: "🔄 Bottom-up — Merge PRs one at a time from bottom", Value: mergeStrategyBottomUp},
		}
		defaultIndex = 0
	} else {
		strategyOptions = []tui.SelectOption{
			{Label: "🔄 Bottom-up — Merge PRs one at a time from bottom (recommended)", Value: mergeStrategyBottomUp},
			{Label: "🔀 Ship — Create single PR with all stack commits for atomic merge", Value: mergeStrategyShip},
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
	case mergeStrategyBottomUp:
		strategy = mergeAction.StrategyBottomUp
	case mergeStrategyShip:
		strategy = mergeAction.StrategyShip
		// Default to waiting (matches `merge ship --wait=true` CLI default)
		wait = true
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
	h.out.Print(planText)
}

// ShowMidStackWarning implements InteractiveHandler.
func (h *InteractiveMergeEventHandler) ShowMidStackWarning(scope string, upstackBranchesInScope []string) {
	h.runner.Pause()
	defer h.runner.Resume()

	h.out.Warn("⚠️  You're mid-stack in scope [%s]", scope)
	h.out.Warn("   Branches above in the same scope won't be merged: %s", strings.Join(upstackBranchesInScope, ", "))
	h.out.Info("   💡 To merge the entire scope, use: stackit merge --scope=%s", scope)
}

// PromptPostMerge implements InteractiveHandler.
func (h *InteractiveMergeEventHandler) PromptPostMerge(hasUncommittedChanges bool, trunkName string) (mergeAction.PostMergeAction, error) {
	h.runner.Pause()
	defer h.runner.Resume()

	h.out.Newline()
	h.out.Success("Merge completed successfully!")

	trunkLabel := fmt.Sprintf("🔄 Switch to trunk and sync (%s)", trunkName)
	if hasUncommittedChanges {
		trunkLabel += " " + style.ColorYellow("(warning: you have local changes)")
	}

	options := []tui.SelectOption{
		{Label: trunkLabel, Value: "trunk-sync"},
		{Label: "Done", Value: mergeStrategyDone},
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

// PromptSimpleMergeConfirm implements InteractiveHandler.
func (h *InteractiveMergeEventHandler) PromptSimpleMergeConfirm(branch mergeAction.BranchMergeInfo, baseBranch string) (bool, error) {
	h.runner.Pause()
	defer h.runner.Resume()

	h.out.Newline()
	h.out.Info("Ready to merge PR #%d: %s", branch.PRNumber, branch.BranchName)
	h.out.Info("Target: %s", baseBranch)
	h.out.Newline()

	return tui.PromptConfirm(fmt.Sprintf("Merge %s into %s?", branch.BranchName, baseBranch), false)
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

	if plan.Strategy == mergeAction.StrategyShip {
		// For ship strategy: group by step type, not by individual branch
		groups = appendShipGroups(groups, plan, assigned)
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

// appendShipGroups appends groups for ship strategy
func appendShipGroups(groups []Group, plan *mergeAction.Plan, assigned map[int]bool) []Group {
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
		// Build set for O(1) lookup instead of O(n) per step
		upstackSet := make(map[string]bool, len(plan.UpstackBranches))
		for _, ub := range plan.UpstackBranches {
			upstackSet[ub] = true
		}

		var indices []int
		for i, step := range plan.Steps {
			if assigned[i] {
				continue
			}
			if upstackSet[step.BranchName] {
				indices = append(indices, i)
				assigned[i] = true
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

// DisplayMergeStatus renders the shippability status of stacks.
// Shows stacks grouped by status (Ready, Pending, Blocked, Incomplete).
func DisplayMergeStatus(out output.Output, result *shippable.AnalysisResult) {
	styles := style.DefaultStatusStyles()

	// Empty state
	if result.TotalStacks() == 0 {
		out.Print("\n")
		out.Print(style.ColorDim("No active stacks found.") + "\n")
		out.Print(style.ColorDim("Create a stack with: stackit create -m \"description\"") + "\n")
		out.Print("\n")
		return
	}

	out.Print("\n")
	out.Print(style.ColorMagenta("📦 Your Mergeable Work") + "\n")

	// Ready stacks
	if ready := result.GetShippable(); len(ready) > 0 {
		out.Print("\n")
		out.Print(styles.Done.Render(fmt.Sprintf("Ready (%d):", len(ready))) + "\n")
		for _, s := range ready {
			detail := "All approved, CI passing"
			recommended := mergeAction.DetermineRecommendedStrategy(s.BranchCount())
			if recommended == mergeAction.StrategyShip {
				detail += " — ship recommended"
			}
			printStackEntry(out, "  ✅ ", s, detail)
		}
	}

	// Pending stacks
	if pending := result.GetPending(); len(pending) > 0 {
		out.Print("\n")
		out.Print(style.ColorYellow(fmt.Sprintf("Pending (%d):", len(pending))) + "\n")
		for _, s := range pending {
			printStackEntry(out, "  ⏳ ", s, getBlockingDetail(s))
		}
	}

	// Blocked stacks
	if blocked := result.GetBlocked(); len(blocked) > 0 {
		out.Print("\n")
		out.Print(styles.Error.Render(fmt.Sprintf("Blocked (%d):", len(blocked))) + "\n")
		for _, s := range blocked {
			printStackEntry(out, "  ❌ ", s, getBlockingDetail(s))
		}
	}

	// Incomplete stacks
	if incomplete := result.GetIncomplete(); len(incomplete) > 0 {
		out.Print("\n")
		out.Print(style.ColorDim(fmt.Sprintf("Incomplete (%d):", len(incomplete))) + "\n")
		for _, s := range incomplete {
			printStackEntry(out, "  ○  ", s, getBlockingDetail(s))
		}
	}

	// Actionable tip
	out.Print("\n")
	if result.HasShippable() {
		out.Print(style.ColorDim("Run 'stackit merge' to start merging ready stacks.") + "\n")
	}
}

// printStackEntry renders a two-line stack entry with title, branch chain, and detail.
func printStackEntry(out output.Output, prefix string, s shippable.Stack, detail string) {
	// Line 1: title
	out.Print(fmt.Sprintf("%s%s\n", prefix, s.DisplayTitle()))

	// Line 2: branch chain with PR numbers and detail
	indent := strings.Repeat(" ", len(prefix))
	chain := formatBranchChain(s)
	out.Print(fmt.Sprintf("%s%s  %s\n", indent, chain, style.ColorDim(detail)))
}

// formatBranchChain formats the branch names as "a -> b -> c" with PR numbers.
func formatBranchChain(s shippable.Stack) string {
	branches := s.Stack.AllBranches
	if len(branches) == 0 {
		return style.ColorDim(fmt.Sprintf("%d branches", s.BranchCount()))
	}

	// Build a map from branch name to PR number for quick lookup
	prMap := make(map[string]int, len(s.BlockingPRs))
	for _, bp := range s.BlockingPRs {
		if bp.PRNumber > 0 {
			prMap[bp.Branch] = bp.PRNumber
		}
	}

	// Collect PR numbers (in branch order) for a compact suffix
	var prNumbers []string
	for _, b := range branches {
		if n, ok := prMap[b]; ok {
			prNumbers = append(prNumbers, fmt.Sprintf("#%d", n))
		}
	}

	// Format branch chain, collapsing middle branches if > 3
	var chain string
	switch {
	case len(branches) <= 3:
		chain = strings.Join(branches, " -> ")
	default:
		chain = fmt.Sprintf("%s -> ... -> %s (%d branches)",
			branches[0], branches[len(branches)-1], len(branches))
	}

	if len(prNumbers) > 0 {
		return chain + "  " + style.ColorDim("(PRs "+strings.Join(prNumbers, ", ")+")")
	}
	return chain
}

// getBlockingDetail returns a human-readable description of what's blocking a stack,
// including the specific PR number when applicable.
func getBlockingDetail(s shippable.Stack) string {
	for _, bp := range s.BlockingPRs {
		prRef := ""
		if bp.PRNumber > 0 {
			prRef = fmt.Sprintf(" on #%d", bp.PRNumber)
		}

		switch bp.Reason {
		case shippable.ReasonCIPending:
			return fmt.Sprintf("CI running%s", prRef)
		case shippable.ReasonReviewRequired:
			return fmt.Sprintf("Review required%s", prRef)
		case shippable.ReasonCIFailing:
			return fmt.Sprintf("CI failed%s", prRef)
		case shippable.ReasonChangesRequested:
			return fmt.Sprintf("Changes requested%s", prRef)
		case shippable.ReasonNotPushed:
			return fmt.Sprintf("Not pushed (%s)", bp.Branch)
		case shippable.ReasonNoPR:
			return fmt.Sprintf("Missing PR (%s)", bp.Branch)
		case shippable.ReasonDraft:
			return fmt.Sprintf("Draft PR%s", prRef)
		}
	}
	return "Unknown"
}
