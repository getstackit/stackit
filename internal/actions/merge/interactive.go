package merge

import (
	"stackit.dev/stackit/internal/engine"
)

// MergeType represents what the user wants to merge
//
//nolint:revive // MergeType is clearer than just Type in this context
type MergeType string

// MergeType constants
const (
	MergeTypeThis   MergeType = "this"   // Merge current branch and its stack
	MergeTypeScope  MergeType = "scope"  // Merge all branches in a scope
	MergeTypeStacks MergeType = "stacks" // Merge selected stacks
)

// PostMergeAction represents a follow-up action after merge
type PostMergeAction string

// PostMergeAction constants
const (
	PostMergeSyncTrunk PostMergeAction = "sync-trunk" // Switch to trunk and sync
	PostMergeDone      PostMergeAction = "done"       // No follow-up action
)

// StrategyChoice contains the user's strategy selection and related options
type StrategyChoice struct {
	Strategy Strategy
	Wait     bool // Whether to wait for CI (consolidate strategy only)
}

// InteractiveHandler extends EventHandler with interactive prompt capabilities.
// This allows the action layer to request user input without depending on
// specific UI implementations (TUI, CLI, etc.).
//
// Implementations should handle TUI lifecycle (pause/resume) internally when
// showing prompts to avoid conflicts with progress display.
//
// Error handling: Prompt methods should return sterrors.ErrCanceled when the
// user cancels (e.g., Ctrl+C, Escape). Other errors indicate actual failures.
type InteractiveHandler interface {
	EventHandler

	// IsInteractive returns true if this handler supports interactive prompts.
	// Non-interactive handlers (e.g., SimpleMergeEventHandler) return false,
	// and their prompt methods will return errors.
	// Check this before calling prompt methods to provide better error messages.
	IsInteractive() bool

	// PromptMergeType asks user what to merge (this/scope/stacks).
	// Called at the start of the wizard when no merge target is pre-selected.
	// canMergeThisBranch indicates if "This branch" is a valid option (false on trunk or empty worktree).
	// Returns ErrCanceled if user cancels.
	PromptMergeType(canMergeThisBranch bool, availableScopes []string, availableStacks []MultiStackInfo) (MergeType, error)

	// PromptScope asks user to select a scope from available options.
	// Called when user selects MergeTypeScope.
	// Returns ErrCanceled if user cancels.
	PromptScope(availableScopes []string) (string, error)

	// PromptStacks asks user to select stacks (multi-select with ordering).
	// Returns selected stack root names in priority order.
	// Called when user selects MergeTypeStacks.
	// Returns ErrCanceled if user cancels.
	PromptStacks(availableStacks []MultiStackInfo) ([]string, error)

	// PromptStrategy asks user to select merge strategy.
	// plan provides context (branch count, etc.) for display.
	// recommended is the suggested default based on stack size.
	// Returns ErrCanceled if user cancels.
	PromptStrategy(plan *Plan, recommended Strategy) (StrategyChoice, error)

	// PromptConfirm asks user to confirm an action.
	// Returns ErrCanceled if user cancels.
	PromptConfirm(message string, defaultYes bool) (bool, error)

	// ShowPlan displays the merge plan for review.
	// This is informational - no return value needed.
	// Called before prompting for strategy selection.
	ShowPlan(plan *Plan, validation *PlanValidation)

	// ShowMidStackWarning displays a warning when user is mid-stack in a scope.
	// Called when branches above the current branch share the same scope.
	ShowMidStackWarning(scope string, upstackBranchesInScope []string)

	// PromptPostMerge asks user what to do after merge completes.
	// Called after successful merge execution.
	// Returns ErrCanceled if user cancels (treated as "done").
	PromptPostMerge(hasUncommittedChanges bool, trunkName string) (PostMergeAction, error)

	// PromptIndividualMerge asks user if they want to merge PRs individually
	// when consolidation was requested but individual merge is possible.
	// This is offered when all branches are leaves (no children) and all PRs
	// are mergeable without conflicts.
	// Returns true for individual merge, false for consolidation.
	PromptIndividualMerge(branches []BranchMergeInfo) (bool, error)

	// PromptSimpleMergeConfirm shows a simplified confirmation for single-branch merges.
	// This provides a cleaner UX than ShowPlan + PromptConfirm for simple cases.
	// Returns true to proceed, false to cancel.
	PromptSimpleMergeConfirm(branch BranchMergeInfo, baseBranch string) (bool, error)
}

// GetAvailableScopes returns all unique non-empty scopes in the repository
func GetAvailableScopes(eng engine.Engine) []string {
	scopes := make(map[string]bool)
	for _, b := range eng.AllBranches() {
		s := eng.GetScope(b).String()
		if s != "" && s != "none" && s != "clear" {
			scopes[s] = true
		}
	}

	result := make([]string, 0, len(scopes))
	for s := range scopes {
		result = append(result, s)
	}
	return result
}

// AnalyzeMidStackScope checks if user is mid-stack in a scope and returns
// any upstack branches that are in the same scope (won't be merged).
func AnalyzeMidStackScope(eng engine.Engine, plan *Plan, currentScope string) []string {
	if currentScope == "" {
		return nil
	}

	var upstackInScope []string
	for _, upstackName := range plan.UpstackBranches {
		upstackBranch := eng.GetBranch(upstackName)
		upstackScope := eng.GetScope(upstackBranch)
		if upstackScope.String() == currentScope {
			upstackInScope = append(upstackInScope, upstackName)
		}
	}
	return upstackInScope
}
