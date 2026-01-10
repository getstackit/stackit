package actions

import (
	"fmt"
	"os"

	"stackit.dev/stackit/internal/app"
	"stackit.dev/stackit/internal/engine"
	"stackit.dev/stackit/internal/git"
	"stackit.dev/stackit/internal/tui/style"
)

// CheckoutOptions contains options for the checkout command
type CheckoutOptions struct {
	BranchName    string // Optional: branch to checkout directly
	ShowUntracked bool   // Include untracked branches in selection
	All           bool   // Show all branches across trunks
	StackOnly     bool   // Only show current stack (ancestors + descendants)
	CheckoutTrunk bool   // Checkout trunk directly
}

// CheckoutResult represents the outcome of a checkout operation.
// CLI layer should use the WorktreeSwitchPath field to derive shell directives.
type CheckoutResult struct {
	// Canceled indicates the user canceled interactive selection
	Canceled bool
	// AlreadyOnBranch indicates checkout was a no-op (already on target branch)
	AlreadyOnBranch bool
	// BranchName is the name of the target branch
	BranchName string
	// WorktreeSwitchPath is set when checkout requires switching worktrees (CLI emits CD directive)
	WorktreeSwitchPath string
	// RerunArgs are args to rerun stackit after worktree switch (CLI emits Rerun directive)
	RerunArgs []string
	// FallbackTip is shown when worktree switch is needed but shell integration is not available
	FallbackTip string
}

// CheckoutHandler abstracts TTY vs non-TTY interactions for checkout operations
type CheckoutHandler interface {
	// SelectBranch prompts the user to select a branch interactively.
	// Returns the selected branch name, or an error if selection failed or was canceled.
	SelectBranch(ctx *app.Context, opts CheckoutOptions) (string, error)
}

// NullCheckoutHandler is a no-op handler that returns an error for interactive operations
type NullCheckoutHandler struct{}

// SelectBranch returns an error since interactive selection is not available
func (h *NullCheckoutHandler) SelectBranch(_ *app.Context, _ CheckoutOptions) (string, error) {
	return "", fmt.Errorf("interactive branch selection is not available; please specify a branch name")
}

// CheckoutAction performs the checkout operation.
// Returns a CheckoutResult with data that CLI can use to determine shell directives.
func CheckoutAction(ctx *app.Context, opts CheckoutOptions, handler CheckoutHandler) (CheckoutResult, error) {
	eng := ctx.Engine
	out := ctx.Output
	context := ctx.Context

	// Use null handler if none provided
	if handler == nil {
		handler = &NullCheckoutHandler{}
	}

	var branchName string
	var err error

	switch {
	case opts.CheckoutTrunk:
		branchName = eng.Trunk().GetName()
	case opts.BranchName != "":
		branchName = opts.BranchName
	default:
		// Only populate remote SHAs when entering interactive mode
		// (the selector may need remote information for display)
		if err := eng.PopulateRemoteShas(); err != nil {
			return CheckoutResult{}, fmt.Errorf("failed to populate remote SHAs: %w", err)
		}

		branchName, err = handler.SelectBranch(ctx, opts)
		if err != nil {
			return CheckoutResult{}, err
		}
		if branchName == "" {
			// User canceled - stay on current branch
			currentBranch := eng.CurrentBranch()
			currentBranchName := "trunk"
			if currentBranch != nil {
				currentBranchName = currentBranch.GetName()
			}
			out.Info("No branch selected; staying on %s.", style.ColorBranchName(currentBranchName, true))
			return CheckoutResult{Canceled: true, BranchName: currentBranchName}, nil
		}
	}

	currentBranch := eng.CurrentBranch()
	if currentBranch != nil && branchName == currentBranch.GetName() {
		if !ctx.Quiet {
			out.Info("Already on %s.", style.ColorBranchName(branchName, true))
		}
		return CheckoutResult{AlreadyOnBranch: true, BranchName: branchName}, nil
	}

	branch := eng.GetBranch(branchName)

	// Block checking out worktree anchor branches directly
	if branch.IsWorktreeAnchor() {
		// Find the worktree name for a better error message
		wtInfo, _ := eng.GetWorktreeForStack(branchName)
		displayName := branchName // Default to branch name for legacy worktrees
		if wtInfo != nil && wtInfo.Name != "" {
			displayName = wtInfo.Name
		}
		return CheckoutResult{}, fmt.Errorf("cannot check out worktree anchor branch directly; use 'cd $(stackit worktree open %s)' to enter the worktree", displayName)
	}

	// Check if checkout requires worktree switching (returns info for CLI to emit directives)
	switchPath, rerunArgs, fallbackTip := getWorktreeSwitchInfo(ctx, branch, branchName)
	if switchPath != "" {
		// Worktree switch needed - return info for CLI to emit directives
		return CheckoutResult{
			BranchName:         branchName,
			WorktreeSwitchPath: switchPath,
			RerunArgs:          rerunArgs,
			FallbackTip:        fallbackTip,
		}, nil
	}

	if err := eng.CheckoutBranch(context, branch); err != nil {
		if git.IsLocalChangesError(err) {
			return CheckoutResult{}, fmt.Errorf("cannot checkout branch %s because you have uncommitted changes that would be overwritten; please commit or stash your changes before switching branches", branchName)
		}
		return CheckoutResult{}, fmt.Errorf("failed to checkout branch %s: %w", branchName, err)
	}

	previousBranch := "trunk"
	if currentBranch != nil {
		previousBranch = currentBranch.GetName()
	}
	ctx.Logger.Info("branch changed", "from", previousBranch, "to", branchName)

	out.Info("Checked out %s.", style.ColorBranchName(branchName, false))

	// Skip branch info in quiet mode for faster checkout
	if !ctx.Quiet {
		printBranchInfo(ctx, branch)
	}

	return CheckoutResult{BranchName: branchName}, nil
}

func printBranchInfo(ctx *app.Context, branch engine.Branch) {
	if branch.IsTrunk() {
		return
	}

	if !branch.IsTracked() {
		ctx.Output.Info("This branch is not tracked by Stackit.")
		return
	}

	if !branch.IsBranchUpToDate() {
		parent := branch.GetParentPrecondition()
		ctx.Output.Info("This branch has fallen behind %s - you may want to %s.",
			style.ColorBranchName(parent, false),
			style.ColorCyan("stackit upstack restack"))
		return
	}

	// Check if any downstack branch needs restack
	// Limit to checking up to 10 ancestors to avoid performance issues with deep stacks
	rng := engine.StackRange{
		RecursiveParents:  true,
		IncludeCurrent:    false,
		RecursiveChildren: false,
	}
	graph := engine.BuildStackGraph(ctx.Engine, engine.SortStrategyAlphabetical, nil)
	downstack := graph.Range(branch, rng)

	// Limit the number of branches we check to avoid slow metadata reads
	const maxDownstackChecks = 10
	startIdx := len(downstack) - maxDownstackChecks
	if startIdx < 0 {
		startIdx = 0
	}

	// Check from trunk upward (but limit to last maxDownstackChecks branches)
	for i := len(downstack) - 1; i >= startIdx; i-- {
		ancestor := downstack[i]
		if !ancestor.IsBranchUpToDate() {
			parent := ancestor.GetParentPrecondition()
			ctx.Output.Info("The downstack branch %s has fallen behind %s - you may want to %s.",
				style.ColorBranchName(ancestor.GetName(), false),
				style.ColorBranchName(parent, false),
				style.ColorCyan("stackit stack restack"))
			return
		}
	}
}

// getWorktreeSwitchInfo checks if the target branch belongs to a stack with a worktree,
// or if we need to switch back to the main repo from a worktree.
// Returns (switchPath, rerunArgs, fallbackTip) - all empty if no worktree switch is needed.
// The CLI layer is responsible for checking shell integration and emitting directives or fallback tips.
func getWorktreeSwitchInfo(ctx *app.Context, branch engine.Branch, branchName string) (string, []string, string) {
	// Get target branch's stack root
	targetStackRoot := ctx.Engine.GetStackRootForBranch(branch)

	// Case 1: We're in a worktree and checking out a branch NOT in this worktree's stack
	// (either trunk, or a branch from a different stack)
	if ctx.InManagedWorktree && ctx.WorktreeInfo != nil {
		currentStackRoot := ctx.WorktreeInfo.AnchorBranch

		// Check if target is in a different location than current worktree
		var switchTarget string
		var fallbackTip string

		if targetStackRoot == "" {
			// Target is trunk or untracked - switch to main repo
			switchTarget = ctx.WorktreeInfo.MainRepoDir
			fallbackTip = fmt.Sprintf("cd %s && stackit co %s", switchTarget, branchName)
		} else if targetStackRoot != currentStackRoot {
			// Target is in a different stack - check if that stack has a worktree
			targetWorktree, err := ctx.Engine.GetWorktreeForStack(targetStackRoot)
			if err == nil && targetWorktree != nil {
				switchTarget = targetWorktree.Path
			} else {
				// Target stack has no worktree - switch to main repo
				switchTarget = ctx.WorktreeInfo.MainRepoDir
			}
			fallbackTip = fmt.Sprintf("cd %s && stackit co %s", switchTarget, branchName)
		}

		if switchTarget != "" {
			ctx.Output.Info("Switching to %s.", switchTarget)
			return switchTarget, []string{"co", branchName}, fallbackTip
		}
		// Target is in current worktree's stack - proceed with normal checkout
		return "", nil, ""
	}

	// Case 2: We're in main repo and checking out a branch that has a worktree
	if targetStackRoot == "" {
		return "", nil, "" // Target is trunk or untracked, no worktree needed
	}

	// Check if this stack has a registered worktree
	targetWorktree, err := ctx.Engine.GetWorktreeForStack(targetStackRoot)
	if err != nil || targetWorktree == nil {
		return "", nil, "" // No worktree for this stack
	}

	// Verify worktree path exists
	if _, err := os.Stat(targetWorktree.Path); os.IsNotExist(err) {
		ctx.Output.Warn("Worktree for stack %s is registered but path does not exist: %s",
			style.ColorBranchName(targetStackRoot, false), targetWorktree.Path)
		ctx.Output.Tip("stackit worktree remove %s", targetStackRoot)
		return "", nil, "" // Fall back to normal checkout
	}

	// Return info for CLI to emit directives
	ctx.Output.Info("Switching to worktree for stack %s.", style.ColorBranchName(targetStackRoot, false))
	fallbackTip := fmt.Sprintf("cd %s && stackit co %s\nFor automatic worktree switching, enable shell integration: eval \"$(stackit shell zsh)\"", targetWorktree.Path, branchName)
	return targetWorktree.Path, []string{"co", branchName}, fallbackTip
}
