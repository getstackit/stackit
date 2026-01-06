package actions

import (
	"fmt"
	"os"

	"stackit.dev/stackit/internal/app"
	"stackit.dev/stackit/internal/engine"
	"stackit.dev/stackit/internal/errors"
	"stackit.dev/stackit/internal/git"
	"stackit.dev/stackit/internal/tui"
	"stackit.dev/stackit/internal/tui/style"
	"stackit.dev/stackit/internal/utils"
)

// CheckoutOptions contains options for the checkout command
type CheckoutOptions struct {
	BranchName    string // Optional: branch to checkout directly
	ShowUntracked bool   // Include untracked branches in selection
	All           bool   // Show all branches across trunks
	StackOnly     bool   // Only show current stack (ancestors + descendants)
	CheckoutTrunk bool   // Checkout trunk directly
}

// CheckoutAction performs the checkout operation
func CheckoutAction(ctx *app.Context, opts CheckoutOptions) error {
	eng := ctx.Engine
	out := ctx.Output
	context := ctx.Context

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
			return fmt.Errorf("failed to populate remote SHAs: %w", err)
		}

		if !utils.IsInteractive() {
			return fmt.Errorf("interactive branch selection is not available in non-interactive mode; please specify a branch name")
		}
		branchName, err = tui.PromptLogSelect(ctx.Context, ctx.Engine, ctx.GitHubClient, tui.LogOptions{
			Style:         "FULL", // Show stats by default in checkout selector
			ShowUntracked: opts.ShowUntracked,
		})
		if err != nil {
			if errors.Is(err, errors.ErrCanceled) {
				currentBranch := eng.CurrentBranch()
				currentBranchName := "trunk"
				if currentBranch != nil {
					currentBranchName = currentBranch.GetName()
				}
				out.Info("No branch selected; staying on %s.", style.ColorBranchName(currentBranchName, true))
				return nil
			}
			return err
		}
	}

	currentBranch := eng.CurrentBranch()
	if currentBranch != nil && branchName == currentBranch.GetName() {
		out.Info("Already on %s.", style.ColorBranchName(branchName, true))
		return nil
	}

	branch := eng.GetBranch(branchName)

	// Handle checkout via worktree directives if the branch belongs to a stack with a worktree
	if handleWorktreeCheckout(ctx, branch, branchName) {
		return nil // Checkout handled via shell directives
	}

	if err := eng.CheckoutBranch(context, branch); err != nil {
		if git.IsLocalChangesError(err) {
			return fmt.Errorf("cannot checkout branch %s because you have uncommitted changes that would be overwritten; please commit or stash your changes before switching branches", branchName)
		}
		return fmt.Errorf("failed to checkout branch %s: %w", branchName, err)
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

	return nil
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

// handleWorktreeCheckout checks if the target branch belongs to a stack with a worktree,
// or if we need to switch back to the main repo from a worktree.
// If shell integration is available, emits DirectiveCD + DirectiveRerun for the shell wrapper.
// Otherwise, shows a warning with cd tip.
// Returns true if handled via directives (caller should skip git checkout).
func handleWorktreeCheckout(ctx *app.Context, branch engine.Branch, branchName string) bool {
	// Get target branch's stack root
	targetStackRoot := ctx.Engine.GetStackRootForBranch(branch)

	// Case 1: We're in a worktree and checking out a branch NOT in this worktree's stack
	// (either trunk, or a branch from a different stack)
	if ctx.InManagedWorktree && ctx.WorktreeInfo != nil {
		currentStackRoot := ctx.WorktreeInfo.StackRoot

		// Check if target is in a different location than current worktree
		needsSwitch := false
		var switchTarget string
		var switchMessage string

		if targetStackRoot == "" {
			// Target is trunk or untracked - switch to main repo
			needsSwitch = true
			switchTarget = ctx.WorktreeInfo.MainRepoDir
			switchMessage = "Switching to main repository."
		} else if targetStackRoot != currentStackRoot {
			// Target is in a different stack - check if that stack has a worktree
			targetWorktree, err := ctx.Engine.GetWorktreeForStack(targetStackRoot)
			if err == nil && targetWorktree != nil {
				needsSwitch = true
				switchTarget = targetWorktree.Path
				switchMessage = fmt.Sprintf("Switching to worktree for stack %s.", style.ColorBranchName(targetStackRoot, false))
			} else {
				// Target stack has no worktree - switch to main repo
				needsSwitch = true
				switchTarget = ctx.WorktreeInfo.MainRepoDir
				switchMessage = "Switching to main repository."
			}
		}

		if needsSwitch {
			if !hasShellIntegration() {
				ctx.Output.Warn("Branch %s is not in this worktree's stack.", style.ColorBranchName(branchName, false))
				ctx.Output.Tip("cd %s && stackit co %s", switchTarget, branchName)
				return false
			}
			ctx.Output.Info(switchMessage)
			ctx.Output.DirectiveCD(switchTarget)
			ctx.Output.DirectiveRerun("co", branchName)
			return true
		}
		// Target is in current worktree's stack - proceed with normal checkout
		return false
	}

	// Case 2: We're in main repo and checking out a branch that has a worktree
	if targetStackRoot == "" {
		return false // Target is trunk or untracked, no worktree needed
	}

	// Check if this stack has a registered worktree
	targetWorktree, err := ctx.Engine.GetWorktreeForStack(targetStackRoot)
	if err != nil || targetWorktree == nil {
		return false // No worktree for this stack
	}

	// Verify worktree path exists
	if _, err := os.Stat(targetWorktree.Path); os.IsNotExist(err) {
		ctx.Output.Warn("Worktree for stack %s is registered but path does not exist: %s",
			style.ColorBranchName(targetStackRoot, false), targetWorktree.Path)
		ctx.Output.Tip("stackit worktree remove %s", targetStackRoot)
		return false // Fall back to normal checkout
	}

	// Check if shell integration is available
	if !hasShellIntegration() {
		// No shell integration - show warning and cd tip, but don't emit directives
		ctx.Output.Warn("Branch %s belongs to stack %s which has a worktree.",
			style.ColorBranchName(branchName, false),
			style.ColorBranchName(targetStackRoot, false))
		ctx.Output.Tip("cd %s && stackit co %s", targetWorktree.Path, branchName)
		ctx.Output.Tip("For automatic worktree switching, enable shell integration: eval \"$(stackit shell zsh)\"")
		return false // Fall back to normal checkout (will result in detached HEAD)
	}

	// Emit directives for shell wrapper to handle
	ctx.Output.Info("Switching to worktree for stack %s.", style.ColorBranchName(targetStackRoot, false))
	ctx.Output.DirectiveCD(targetWorktree.Path)
	ctx.Output.DirectiveRerun("co", branchName)
	return true
}

// hasShellIntegration checks if stackit shell integration is installed.
// The shell wrapper sets STACKIT_SHELL_INTEGRATION=1 when running commands.
func hasShellIntegration() bool {
	return os.Getenv("STACKIT_SHELL_INTEGRATION") == "1"
}
