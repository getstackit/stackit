package actions

import (
	"fmt"
	"os"
	"strings"

	"github.com/sahilm/fuzzy"

	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/internal/output"
)

// CheckoutOptions contains options for the checkout command
type CheckoutOptions struct {
	BranchName         string // Optional: branch to checkout directly
	ShowUntracked      bool   // Include untracked branches in selection
	All                bool   // Show all branches across trunks
	StackOnly          bool   // Only show current stack (ancestors + descendants)
	CheckoutTrunk      bool   // Checkout trunk directly
	SkipWorktreeSwitch bool   // Skip worktree switch logic (for fallback checkout)
}

// CheckoutResult represents the outcome of a checkout operation.
type CheckoutResult struct {
	WorktreeSwitchPath string
	RerunArgs          []string
	FallbackTips       []string
	TargetBranch       string
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
		branchName, err = resolveBranchName(eng, out, opts.BranchName)
		if err != nil {
			return CheckoutResult{}, err
		}
	default:
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
			out.Info("No branch selected; staying on %s.", output.CurrentBranch(currentBranchName))
			return CheckoutResult{}, nil
		}
	}

	currentBranch := eng.CurrentBranch()
	if currentBranch != nil && branchName == currentBranch.GetName() {
		if !ctx.Quiet {
			out.Info("Already on %s.", output.CurrentBranch(branchName))
		}
		return CheckoutResult{}, nil
	}

	branch := eng.GetBranch(branchName)

	// Block checking out worktree anchor branches directly
	if branch.IsWorktreeAnchor() {
		// Find the worktree name for a better error message
		wtInfo, _ := eng.GetWorktreeForStack(branchName)
		displayName := branchName
		if wtInfo != nil && wtInfo.Name != "" {
			displayName = wtInfo.Name
		}
		return CheckoutResult{}, fmt.Errorf("cannot check out worktree anchor branch directly; use 'cd $(stackit worktree open %s)' to enter the worktree", displayName)
	}

	if !opts.SkipWorktreeSwitch {
		switchPath, rerunArgs, fallbackTips := getWorktreeSwitchInfo(ctx, branch, branchName)
		if switchPath != "" {
			return CheckoutResult{
				WorktreeSwitchPath: switchPath,
				RerunArgs:          rerunArgs,
				FallbackTips:       fallbackTips,
				TargetBranch:       branchName,
			}, nil
		}
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
	ctx.Logger.Info("branch changed from=%v to=%v", previousBranch, branchName)

	out.Info("Checked out %s.", output.BranchName(branchName))

	// Skip branch info in quiet mode for faster checkout
	if !ctx.Quiet {
		printBranchInfo(ctx, branch)
	}

	return CheckoutResult{}, nil
}

func printBranchInfo(ctx *app.Context, branch engine.Branch) {
	if branch.IsTrunk() {
		return
	}

	if !branch.IsTracked() {
		ctx.Output.Info("This branch is not tracked by Stackit.")
		return
	}

	// Check if any downstack branch needs restack
	// Limit to checking up to 10 ancestors to avoid performance issues with deep stacks
	rng := engine.StackRange{
		RecursiveParents:  true,
		IncludeCurrent:    false,
		RecursiveChildren: false,
	}
	graph := ctx.Engine.Graph(engine.SortStrategyAlphabetical)
	downstack := graph.Range(branch, rng)

	// Limit the number of branches we check to avoid slow metadata reads
	const maxDownstackChecks = 10
	downstackToCheck := downstack.Last(maxDownstackChecks).Reverse()
	statuses := ctx.Engine.ReadBranchStatuses(engine.BranchesOf(branch).Append(downstackToCheck...))

	if !statuses.IsUpToDate(branch) {
		parent := branch.GetParentOrTrunk()
		ctx.Output.Info("This branch has fallen behind %s - you may want to %s.",
			output.BranchName(parent),
			output.Cyan("stackit restack --upstack"))
		return
	}

	// Check nearest ancestors first (but limit to last maxDownstackChecks branches)
	for _, ancestor := range downstackToCheck {
		if !statuses.IsUpToDate(ancestor) {
			parent := ancestor.GetParentOrTrunk()
			ctx.Output.Info("The downstack branch %s has fallen behind %s - you may want to %s.",
				output.BranchName(ancestor.GetName()),
				output.BranchName(parent),
				output.Cyan("stackit restack"))
			return
		}
	}
}

// getWorktreeSwitchInfo checks if the target branch belongs to a stack with a worktree,
// or if we need to switch back to the main repo from a worktree.
// Returns (switchPath, rerunArgs, fallbackTips) - all empty if no worktree switch is needed.
func getWorktreeSwitchInfo(ctx *app.Context, branch engine.Branch, branchName string) (string, []string, []string) {
	targetStackRoot := ctx.Engine.GetStackRootForBranch(branch)

	// Case 1: We're in a worktree and checking out a branch NOT in this worktree's stack
	if ctx.InManagedWorktree && ctx.WorktreeInfo != nil {
		currentStackRoot := ctx.WorktreeInfo.AnchorBranch

		var switchTarget string
		var targetStack string // empty means main repo

		if targetStackRoot == "" {
			// Target is trunk or untracked - switch to main repo
			switchTarget = ctx.WorktreeInfo.MainRepoDir
		} else if targetStackRoot != currentStackRoot {
			// Target is in a different stack - check if that stack has a worktree
			targetWorktree, err := ctx.Engine.GetWorktreeForStack(targetStackRoot)
			if err == nil && targetWorktree != nil {
				switchTarget = targetWorktree.Path
				targetStack = targetStackRoot
			} else {
				// Target stack has no worktree - switch to main repo
				switchTarget = ctx.WorktreeInfo.MainRepoDir
			}
		}

		if switchTarget != "" {
			if targetStack != "" {
				ctx.Output.Info("Switching to worktree for stack %s.", output.BranchName(targetStack))
			} else {
				ctx.Output.Info("Switching to main repository.")
			}
			fallbackTip := fmt.Sprintf("cd %s && stackit co %s", switchTarget, branchName)
			return switchTarget, []string{"co", branchName}, []string{fallbackTip}
		}
		return "", nil, nil
	}

	// Case 2: We're in main repo and checking out a branch that has a worktree
	if targetStackRoot == "" {
		return "", nil, nil
	}

	targetWorktree, err := ctx.Engine.GetWorktreeForStack(targetStackRoot)
	if err != nil || targetWorktree == nil {
		return "", nil, nil
	}

	if _, err := os.Stat(targetWorktree.Path); os.IsNotExist(err) {
		ctx.Output.Warn("Worktree for stack %s is registered but path does not exist: %s",
			output.BranchName(targetStackRoot), targetWorktree.Path)
		ctx.Output.Tip("stackit worktree remove %s", targetStackRoot)
		return "", nil, nil
	}

	ctx.Output.Info("Switching to worktree for stack %s.", output.BranchName(targetStackRoot))
	fallbackTips := []string{
		fmt.Sprintf("cd %s && stackit co %s", targetWorktree.Path, branchName),
		"For automatic worktree switching, enable shell integration: eval \"$(stackit shell zsh)\"",
	}
	return targetWorktree.Path, []string{"co", branchName}, fallbackTips
}

// resolveBranchName resolves a user input to a branch name.
// It tries: exact match → scope match (topmost branch) → fuzzy match.
func resolveBranchName(eng engine.Engine, out output.Output, input string) (string, error) {
	branchNames := eng.BranchNames()
	if branchNames.Contains(input) {
		return input, nil
	}

	allBranches := eng.AllBranches()

	// Try as scope - find topmost branch with this scope
	scopeBranches := engine.Branches{}
	for _, b := range allBranches {
		if !b.IsTrunk() && eng.GetScope(b).String() == input {
			scopeBranches = scopeBranches.Append(b)
		}
	}
	if len(scopeBranches) > 0 {
		sorted := eng.SortBranchesTopologically(scopeBranches)
		topmost := sorted[len(sorted)-1].GetName()
		out.Info("Matched scope %s.", output.Dim(input))
		return topmost, nil
	}

	// Try fuzzy match
	names := branchNames.Names()
	matches := fuzzy.Find(input, names)
	if len(matches) == 1 {
		out.Info("Fuzzy matched to %s.", output.BranchName(matches[0].Str))
		return matches[0].Str, nil
	} else if len(matches) > 1 {
		// Multiple matches - return error with suggestions
		limit := min(5, len(matches))
		suggestions := make([]string, limit)
		for i := range limit {
			suggestions[i] = matches[i].Str
		}
		return "", fmt.Errorf("ambiguous match %q - did you mean: %s", input, strings.Join(suggestions, ", "))
	}

	return "", fmt.Errorf("no branch found matching %q", input)
}
