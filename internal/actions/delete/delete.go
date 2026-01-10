// Package delete provides functionality for deleting branches and their metadata.
package delete

import (
	"fmt"
	"os"

	"stackit.dev/stackit/internal/actions"
	"stackit.dev/stackit/internal/app"
	"stackit.dev/stackit/internal/engine"
	"stackit.dev/stackit/internal/tui/style"
)

// Options contains options for deleting branches
type Options struct {
	BranchName string
	Downstack  bool
	Force      bool
	Upstack    bool
}

// Action deletes a branch and its metadata.
func Action(ctx *app.Context, opts Options, handler Handler) (Result, error) {
	eng := ctx.Engine
	out := ctx.Output

	// Use null handler if none provided
	if handler == nil {
		handler = &NullHandler{}
	}
	defer handler.Cleanup()

	branchName := opts.BranchName
	if branchName == "" {
		currentBranch := eng.CurrentBranch()
		if currentBranch == nil {
			return Result{}, fmt.Errorf("no branch specified and not on a branch")
		}
		branchName = currentBranch.GetName()
	}

	if branchName == "" {
		return Result{}, fmt.Errorf("no branch specified and not on a branch")
	}

	branch := eng.GetBranch(branchName)
	if branch.IsTrunk() {
		return Result{}, fmt.Errorf("cannot delete trunk branch %s", branchName)
	}

	if !branch.IsTracked() {
		return Result{}, fmt.Errorf("branch %s is not tracked by stackit", branchName)
	}

	if branch.IsWorktreeAnchor() {
		return Result{}, fmt.Errorf("cannot delete branch %s because it is a worktree anchor; use 'stackit worktree remove' to remove the worktree first", branchName)
	}

	// Build StackGraph for efficient traversals
	graph := engine.BuildStackGraph(eng, engine.SortStrategyAlphabetical, nil)

	// Determine branches to delete
	toDelete := []engine.Branch{branch}

	if opts.Upstack {
		upstack := graph.Range(branch, engine.StackRange{RecursiveChildren: true})
		toDelete = append(toDelete, upstack...)
	}

	if opts.Downstack {
		downstack := graph.Range(branch, engine.StackRange{RecursiveParents: true})
		toDelete = append(downstack, toDelete...)
	}

	handler.Start(len(toDelete))

	// Confirm if not forced and not merged/closed
	if !opts.Force {
		for _, b := range toDelete {
			shouldDelete, reason := actions.ShouldDeleteBranch(ctx.Context, b.GetName(), eng, false)
			if !shouldDelete {
				// If handler is interactive, prompt for confirmation
				if handler.IsInteractive() {
					confirmed, err := handler.PromptConfirm(b.GetName(), reason)
					if err != nil {
						return Result{}, err
					}
					if !confirmed {
						handler.OnBranch(b.GetName(), StatusSkipped, nil)
						handler.Complete(0, 1)
						return Result{}, nil
					}
				} else if reason == "" {
					return Result{}, fmt.Errorf("branch %s is not merged/closed; use --force to delete anyway", b.GetName())
				}
			}
		}
	}

	// Track children that will need restacking (only for the last branch in the stack if deleting multiple)
	// Actually, if we delete a middle branch, its children are reparented to its parent.
	// If we delete a whole stack, only children of the stack need restacking onto the stack's parent.

	// Delete branches and get children to restack
	childrenToRestack, err := eng.DeleteBranches(ctx.Context, toDelete)
	if err != nil {
		return Result{}, err
	}

	// Batch delete remote metadata for deleted branches
	branchNames := make([]string, len(toDelete))
	for i, b := range toDelete {
		branchNames[i] = b.GetName()
	}
	if err := eng.Git().BatchDeleteRemoteMetadataRefs(branchNames); err != nil {
		out.Debug("Failed to batch delete remote metadata: %v", err)
	}

	for _, name := range branchNames {
		handler.OnBranch(name, StatusDeleted, nil)
		out.Info("Deleted branch %s", style.ColorBranchName(name, false))
	}

	// Identify stack roots that were deleted (branches whose parent is trunk)
	// and clean up any associated worktrees
	deletedStackRoots := []string{}
	trunkName := eng.Trunk().GetName()
	for _, b := range toDelete {
		parent := b.GetParent()
		if parent == nil || parent.GetName() == trunkName {
			deletedStackRoots = append(deletedStackRoots, b.GetName())
		}
	}

	// Cleanup worktrees and get path if user was in a deleted worktree
	var mainRepoDirForSwitch string
	if len(deletedStackRoots) > 0 {
		mainRepoDirForSwitch = cleanupWorktreesForDeletedStacks(ctx, deletedStackRoots)
	}

	// Restack children if any
	if len(childrenToRestack) > 0 {
		handler.OnRestack(len(childrenToRestack))
		out.Info("Restacking children of deleted %s...", actions.Pluralize("branch", len(toDelete)))
		// Convert []string to []Branch for RestackBranches
		branches := make([]engine.Branch, len(childrenToRestack))
		for i, name := range childrenToRestack {
			branches[i] = eng.GetBranch(name)
		}
		if err := actions.RestackBranches(ctx, branches); err != nil {
			return Result{}, fmt.Errorf("failed to restack children: %w", err)
		}
	}

	handler.Complete(len(toDelete), 0)
	return Result{MainRepoDirForSwitch: mainRepoDirForSwitch}, nil
}

// cleanupWorktreesForDeletedStacks removes worktrees for stack roots that have been deleted.
// Best-effort - errors are logged but don't fail the delete operation.
func cleanupWorktreesForDeletedStacks(ctx *app.Context, deletedStackRoots []string) string {
	var mainRepoDir string

	for _, stackRoot := range deletedStackRoots {
		wt, err := ctx.Engine.GetWorktreeForStack(stackRoot)
		if err != nil || wt == nil {
			continue // No worktree registered for this stack
		}

		// Check if user is in this worktree - we'll need to navigate back to main repo
		if ctx.InManagedWorktree && ctx.WorktreeInfo != nil &&
			ctx.WorktreeInfo.AnchorBranch == stackRoot {
			mainRepoDir = wt.MainRepoDir
		}

		ctx.Output.Info("Removing worktree for deleted stack %s", style.ColorBranchName(stackRoot, false))

		// Remove worktree directory if it exists
		if _, statErr := os.Stat(wt.Path); statErr == nil {
			if removeErr := ctx.Engine.RemoveWorktree(ctx.Context, wt.Path); removeErr != nil {
				ctx.Output.Debug("Failed to remove worktree at %s: %v", wt.Path, removeErr)
			}
		}

		// Unregister the worktree from the registry
		if unregErr := ctx.Engine.UnregisterWorktree(stackRoot); unregErr != nil {
			ctx.Output.Debug("Failed to unregister worktree for %s: %v", stackRoot, unregErr)
		}
	}

	return mainRepoDir
}
