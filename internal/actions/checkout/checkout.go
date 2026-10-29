// Package checkout implements the stackit checkout command: resolving a target
// branch, switching worktrees when the branch lives elsewhere, and checking it
// out.
package checkout

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/sahilm/fuzzy"

	"github.com/getstackit/stackit/internal/actions"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/internal/output"
)

// Options contains options for the checkout command
type Options struct {
	BranchName         string // Optional: branch to checkout directly
	ShowUntracked      bool   // Include untracked branches in selection
	All                bool   // Show all branches across trunks
	StackOnly          bool   // Only show current stack (ancestors + descendants)
	CheckoutTrunk      bool   // Checkout trunk directly
	SkipWorktreeSwitch bool   // Skip worktree switch logic (for fallback checkout)
}

// Result represents the outcome of a checkout operation.
type Result struct {
	WorktreeSwitchPath string
	RerunArgs          []string
	FallbackTips       []string
	TargetBranch       string
}

// Handler abstracts TTY vs non-TTY interactions for checkout operations
type Handler interface {
	// SelectBranch prompts the user to select a branch interactively.
	// Returns the selected branch name, or an error if selection failed or was canceled.
	SelectBranch(ctx context.Context, opts Options) (string, error)
}

// NullHandler is a no-op handler that returns an error for interactive operations
type NullHandler struct{}

// SelectBranch returns an error since interactive selection is not available
func (h *NullHandler) SelectBranch(context.Context, Options) (string, error) {
	return "", errors.New("interactive branch selection is not available; please specify a branch name")
}

// Engine lists exactly the engine methods checkout calls.
type Engine interface {
	engine.BranchLookup
	Trunk() engine.Branch
	CurrentBranch(ctx context.Context) *engine.Branch
	AllBranches() engine.Branches
	BranchNames() *engine.BranchSet
	GetScope(branch engine.Branch) engine.Scope
	SortBranchesTopologically(ctx context.Context, branches engine.Branches) engine.Branches
	Graph(ctx context.Context, strategy engine.SortStrategy) *engine.StackGraph
	ReadBranchStatuses(ctx context.Context, branches engine.Branches) engine.BranchStatuses
	GetStackRootForBranch(branch engine.Branch) string
	GetWorktreeForStack(ctx context.Context, stackRoot string) (*engine.WorktreeInfo, error)
	CheckoutBranch(ctx context.Context, branch engine.Branch) error
}

// Deps are the collaborators checkout needs. Adapters build them from their
// own runtime state; the action never sees bootstrap types.
type Deps struct {
	Engine Engine
	Out    output.Output
	Log    output.Logger
	// Quiet skips the "already on" notice and the post-checkout branch info.
	Quiet bool
	Here  actions.Here
}

// Action performs the checkout operation.
func Action(ctx context.Context, deps Deps, opts Options, handler Handler) (Result, error) {
	eng := deps.Engine
	out := deps.Out

	// Use null handler if none provided
	if handler == nil {
		handler = &NullHandler{}
	}

	var branchName string
	var err error

	switch {
	case opts.CheckoutTrunk:
		branchName = eng.Trunk().Name()
	case opts.BranchName != "":
		branchName, err = resolveBranchName(ctx, eng, out, opts.BranchName)
		if err != nil {
			return Result{}, err
		}
	default:
		branchName, err = handler.SelectBranch(ctx, opts)
		if err != nil {
			return Result{}, err
		}
		if branchName == "" {
			// User canceled - stay on current branch
			currentBranch := eng.CurrentBranch(ctx)
			currentBranchName := "trunk"
			if currentBranch != nil {
				currentBranchName = currentBranch.Name()
			}
			out.Info("No branch selected; staying on %s.", output.CurrentBranch(currentBranchName))
			return Result{}, nil
		}
	}

	currentBranch := eng.CurrentBranch(ctx)
	if currentBranch != nil && branchName == currentBranch.Name() {
		if !deps.Quiet {
			out.Info("Already on %s.", output.CurrentBranch(branchName))
		}
		return Result{}, nil
	}

	branch := eng.GetBranch(branchName)

	// Block checking out worktree anchor branches directly
	if branch.IsWorktreeAnchor() {
		// Find the worktree name for a better error message
		wtInfo, _ := eng.GetWorktreeForStack(ctx, branchName)
		displayName := branchName
		if wtInfo != nil && wtInfo.Name != "" {
			displayName = wtInfo.Name.String()
		}
		return Result{}, fmt.Errorf("cannot check out worktree anchor branch directly; use 'cd $(stackit worktree open %s)' to enter the worktree", displayName)
	}

	if !opts.SkipWorktreeSwitch {
		switchPath, rerunArgs, fallbackTips := getWorktreeSwitchInfo(ctx, deps, branch, branchName)
		if switchPath != "" {
			return Result{
				WorktreeSwitchPath: switchPath,
				RerunArgs:          rerunArgs,
				FallbackTips:       fallbackTips,
				TargetBranch:       branchName,
			}, nil
		}
	}

	if err := eng.CheckoutBranch(ctx, branch); err != nil {
		if git.IsLocalChangesError(err) {
			return Result{}, fmt.Errorf("cannot checkout branch %s because you have uncommitted changes that would be overwritten; please commit or stash your changes before switching branches", branchName)
		}
		return Result{}, fmt.Errorf("failed to checkout branch %s: %w", branchName, err)
	}

	previousBranch := "trunk"
	if currentBranch != nil {
		previousBranch = currentBranch.Name()
	}
	deps.Log.Info("branch changed from=%v to=%v", previousBranch, branchName)

	out.Info("Checked out %s.", output.BranchName(branchName))

	// Skip branch info in quiet mode for faster checkout
	if !deps.Quiet {
		printBranchInfo(ctx, deps, branch)
	}

	return Result{}, nil
}

func printBranchInfo(ctx context.Context, deps Deps, branch engine.Branch) {
	if branch.IsTrunk() {
		return
	}

	if !branch.IsTracked() {
		deps.Out.Info("This branch is not tracked by Stackit.")
		return
	}

	// Check if any downstack branch needs restack
	// Limit to checking up to 10 ancestors to avoid performance issues with deep stacks
	rng := engine.StackRange{
		RecursiveParents:  true,
		IncludeCurrent:    false,
		RecursiveChildren: false,
	}
	graph := deps.Engine.Graph(ctx, engine.SortStrategyAlphabetical)
	downstack := graph.Range(branch, rng)

	// Limit the number of branches we check to avoid slow metadata reads
	const maxDownstackChecks = 10
	downstackToCheck := downstack.Last(maxDownstackChecks).Reverse()
	statuses := deps.Engine.ReadBranchStatuses(ctx, engine.BranchesOf(branch).Append(downstackToCheck...))

	if !statuses.IsUpToDate(branch) {
		parent := branch.ParentOrTrunk()
		deps.Out.Info("This branch has fallen behind %s - you may want to %s.",
			output.BranchName(parent),
			output.Cyan("stackit restack --upstack"))
		return
	}

	// Check nearest ancestors first (but limit to last maxDownstackChecks branches)
	for _, ancestor := range downstackToCheck {
		if !statuses.IsUpToDate(ancestor) {
			parent := ancestor.ParentOrTrunk()
			deps.Out.Info("The downstack branch %s has fallen behind %s - you may want to %s.",
				output.BranchName(ancestor.Name()),
				output.BranchName(parent),
				output.Cyan("stackit restack"))
			return
		}
	}
}

// getWorktreeSwitchInfo checks if the target branch belongs to a stack with a worktree,
// or if we need to switch back to the main repo from a worktree.
// Returns (switchPath, rerunArgs, fallbackTips) - all empty if no worktree switch is needed.
// worktreePathUsable reports whether a registered worktree can actually be
// switched to, warning when it cannot. Both switch directions check this: a
// registration whose directory is gone must not be offered as a destination
// from the main repo or from another worktree.
func worktreePathUsable(out output.Output, stackRoot string, path engine.WorktreePath) bool {
	_, err := os.Stat(path.String())
	switch {
	case err == nil:
		return true
	case errors.Is(err, os.ErrNotExist):
		out.Warn("Worktree for stack %s is registered but path does not exist: %s",
			output.BranchName(stackRoot), path)
		// detach, not remove: remove refuses for any stack that still has
		// branches, which is every stack worth checking out.
		out.Tip("stackit worktree detach %s", stackRoot)
	default:
		out.Warn("Cannot inspect worktree for stack %s at %s: %v",
			output.BranchName(stackRoot), path, err)
	}
	return false
}

func getWorktreeSwitchInfo(ctx context.Context, deps Deps, branch engine.Branch, branchName string) (string, []string, []string) {
	targetStackRoot := deps.Engine.GetStackRootForBranch(branch)

	// Case 1: We're in a worktree and checking out a branch NOT in this worktree's stack
	if deps.Here.InManagedWorktree && deps.Here.Worktree != nil {
		currentStackRoot := deps.Here.Worktree.AnchorBranch

		var switchTarget string
		var targetStack string // empty means main repo

		if targetStackRoot == "" {
			// Target is trunk or untracked - switch to main repo
			switchTarget = deps.Here.Worktree.MainRepoDir
		} else if targetStackRoot != currentStackRoot {
			// Target is in a different stack - check if that stack has a worktree
			targetWorktree, err := deps.Engine.GetWorktreeForStack(ctx, targetStackRoot)
			switch {
			case err == nil && targetWorktree != nil && worktreePathUsable(deps.Out, targetStackRoot, targetWorktree.Path):
				switchTarget = targetWorktree.Path.String()
				targetStack = targetStackRoot
			default:
				// Target stack has no usable worktree - switch to main repo
				switchTarget = deps.Here.Worktree.MainRepoDir
			}
		}

		if switchTarget != "" {
			if targetStack != "" {
				deps.Out.Info("Switching to worktree for stack %s.", output.BranchName(targetStack))
			} else {
				deps.Out.Info("Switching to main repository.")
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

	targetWorktree, err := deps.Engine.GetWorktreeForStack(ctx, targetStackRoot)
	if err != nil || targetWorktree == nil {
		return "", nil, nil
	}

	if !worktreePathUsable(deps.Out, targetStackRoot, targetWorktree.Path) {
		return "", nil, nil
	}

	deps.Out.Info("Switching to worktree for stack %s.", output.BranchName(targetStackRoot))
	fallbackTips := []string{
		fmt.Sprintf("cd %s && stackit co %s", targetWorktree.Path, branchName),
		"For automatic worktree switching, enable shell integration: eval \"$(stackit shell zsh)\"",
	}
	return targetWorktree.Path.String(), []string{"co", branchName}, fallbackTips
}

// resolveBranchName resolves a user input to a branch name.
// It tries: exact match → scope match (topmost branch) → fuzzy match.
func resolveBranchName(ctx context.Context, eng Engine, out output.Output, input string) (string, error) {
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
		sorted := eng.SortBranchesTopologically(ctx, scopeBranches)
		topmost := sorted[len(sorted)-1].Name()
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
