// Package delete provides functionality for deleting branches and their metadata.
package delete

import (
	"fmt"
	"os"
	"slices"

	"github.com/getstackit/stackit/internal/actions"
	"github.com/getstackit/stackit/internal/actions/validation"
	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/output"
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

	// Validate branch can be deleted
	if err := (validation.Chain{
		validation.BranchMustNotBeTrunk(eng, branchName),
		validation.BranchMustBeTracked(eng, branchName),
	}).Validate(); err != nil {
		return Result{}, err
	}

	branch := eng.GetBranch(branchName)

	// Custom worktree anchor check with helpful guidance
	if branch.IsWorktreeAnchor() {
		return Result{}, fmt.Errorf("cannot delete branch %s because it is a worktree anchor; use 'stackit worktree remove' to remove the worktree first", branchName)
	}

	// Build StackGraph for efficient traversals
	graph := eng.Graph(engine.SortStrategyAlphabetical)

	// Determine branches to delete
	toDelete := engine.BranchesOf(branch)

	if opts.Upstack {
		upstack := graph.Range(branch, engine.StackRange{RecursiveChildren: true})
		toDelete = toDelete.Concat(upstack)
	}

	if opts.Downstack {
		downstack := graph.Range(branch, engine.StackRange{RecursiveParents: true})
		toDelete = downstack.Concat(toDelete)
	}

	// Deletion rewrites metadata and can reparent surviving children, so it has
	// the same worktree-ownership boundary as other stack mutations. A caller
	// operating from the owning managed worktree still passes this check, which
	// preserves delete's deliberate full-stack worktree cleanup below.
	managedCleanupAnchors, err := managedCleanupAnchors(ctx, toDelete)
	if err != nil {
		return Result{}, err
	}

	handler.Start(len(toDelete))

	// Precompute deletion statuses once (used for confirmation and divergence-preserving reparenting).
	toDeleteNames := toDelete.Names()
	statuses, err := eng.GetDeletionStatuses(ctx.Context, toDeleteNames)
	if err != nil {
		return Result{}, fmt.Errorf("failed to check deletion statuses: %w", err)
	}

	// Confirm if not forced and not merged/closed
	if !opts.Force {
		for _, b := range toDelete {
			status := statuses.For(b.GetName())
			if !status.SafeToDelete {
				// If handler is interactive, prompt for confirmation
				if handler.IsInteractive() {
					confirmed, err := handler.PromptConfirm(b.GetName(), status.Reason)
					if err != nil {
						return Result{}, err
					}
					if !confirmed {
						handler.OnBranch(b.GetName(), StatusSkipped, nil)
						handler.Complete(0, 1)
						return Result{}, nil
					}
				} else if status.Reason == "" {
					return Result{}, fmt.Errorf("branch %s is not merged/closed; use --force to delete anyway", b.GetName())
				}
			}
		}
	}

	// Preserve divergence for survivors whose parent is being deleted as merged/empty.
	// This mirrors clean-branches behavior and avoids replaying already-merged parent commits.
	preReparentedChildren, err := preReparentChildrenWithPreservedDivergence(ctx, toDelete, statuses)
	if err != nil {
		return Result{}, err
	}

	// Track children that will need restacking (only for the last branch in the stack if deleting multiple)
	// Actually, if we delete a middle branch, its children are reparented to its parent.
	// If we delete a whole stack, only children of the stack need restacking onto the stack's parent.

	// Delete branches and get children to restack
	childrenToRestack, err := eng.DeleteBranches(ctx.Context, toDelete)
	if err != nil {
		return Result{}, err
	}
	childrenToRestack = mergeUniqueBranchNames(childrenToRestack, preReparentedChildren)

	// Batch delete remote metadata for deleted branches
	branchNames := toDeleteNames
	if err := eng.DeleteRemoteMetadataForBranches(ctx.Context, branchNames); err != nil {
		out.Debug("Failed to batch delete remote metadata: %v", err)
	}

	for _, name := range branchNames {
		handler.OnBranch(name, StatusDeleted, nil)
		out.Info("Deleted branch %s", output.BranchName(name))
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
	deletedStackRoots = mergeUniqueBranchNames(deletedStackRoots, managedCleanupAnchors)
	if len(deletedStackRoots) > 0 {
		mainRepoDirForSwitch = cleanupWorktreesForDeletedStacks(ctx, deletedStackRoots)
	}

	// Restack children if any
	if len(childrenToRestack) > 0 {
		handler.OnRestack(len(childrenToRestack))
		out.Info("Restacking children of deleted %s...", actions.Pluralize("branch", len(toDelete)))
		branches := engine.BranchesFromNames(eng, childrenToRestack)
		if err := actions.RestackBranches(ctx, branches); err != nil {
			return Result{}, fmt.Errorf("failed to restack children: %w", err)
		}
	}

	handler.Complete(len(toDelete), 0)
	return Result{MainRepoDirForSwitch: mainRepoDirForSwitch}, nil
}

// managedCleanupAnchors enforces normal worktree ownership unless a caller in
// the main repository is deleting every non-anchor branch owned by a managed
// worktree. That full-stack deletion is delete's intentional cleanup workflow:
// it removes the worktree registration after removing its stack.
func managedCleanupAnchors(ctx *app.Context, toDelete engine.Branches) ([]string, error) {
	selected := make(map[string]bool, len(toDelete))
	for _, branch := range toDelete {
		selected[branch.GetName()] = true
	}

	anchors := make(map[string]bool)
	for _, branch := range toDelete {
		owner, err := ctx.Engine.OwningWorktree(branch)
		if err != nil {
			return nil, fmt.Errorf("cannot determine worktree ownership for branch %s: %w", branch.GetName(), err)
		}
		if owner != nil {
			anchors[owner.AnchorBranch] = true
		}
	}

	cleanup := make([]string, 0, len(anchors))
	for anchor := range anchors {
		complete := !ctx.InManagedWorktree
		for _, candidate := range ctx.Engine.AllBranches() {
			if candidate.IsWorktreeAnchor() {
				continue
			}
			owner, err := ctx.Engine.OwningWorktree(candidate)
			if err != nil {
				return nil, fmt.Errorf("cannot determine worktree ownership for branch %s: %w", candidate.GetName(), err)
			}
			if owner != nil && owner.AnchorBranch == anchor && !selected[candidate.GetName()] {
				complete = false
				break
			}
		}
		if complete {
			cleanup = append(cleanup, anchor)
		}
	}

	guarded := make([]engine.Branch, 0, len(toDelete))
	for _, branch := range toDelete {
		owner, err := ctx.Engine.OwningWorktree(branch)
		if err != nil {
			return nil, fmt.Errorf("cannot determine worktree ownership for branch %s: %w", branch.GetName(), err)
		}
		if owner == nil || !anchors[owner.AnchorBranch] || !slices.Contains(cleanup, owner.AnchorBranch) {
			guarded = append(guarded, branch)
		}
	}
	if err := actions.EnsureCanModifyHere(ctx, guarded...); err != nil {
		return nil, err
	}
	return cleanup, nil
}

func preReparentChildrenWithPreservedDivergence(ctx *app.Context, toDelete engine.Branches, statuses engine.DeletionStatuses) ([]string, error) {
	eng := ctx.Engine
	gctx := ctx.Context
	out := ctx.Output

	toDeleteSet := make(map[string]bool, len(toDelete))
	for _, b := range toDelete {
		toDeleteSet[b.GetName()] = true
	}

	graph := eng.Graph(engine.SortStrategyAlphabetical)
	reparentedChildren := make([]string, 0)
	reparentedSet := make(map[string]bool)
	var moves []engine.BranchParentMove

	for _, deleted := range toDelete {
		deletedName := deleted.GetName()
		status := statuses.For(deletedName)
		if !shouldPreserveDivergenceOnDelete(status.Kind) {
			continue
		}

		targetParentName := deleted.GetParentOrTrunk()
		targetParentName = eng.FindNearestNonExcludedAncestor(targetParentName, func(name string) bool {
			return toDeleteSet[name]
		})

		for _, child := range graph.ChildBranches(deleted) {
			childName := child.GetName()
			if toDeleteSet[childName] {
				continue
			}

			moves = append(moves, engine.BranchParentMove{Branch: childName, NewParent: targetParentName})
			out.Debug("Will pre-reparent %s to %s while preserving divergence", childName, targetParentName)
			if !reparentedSet[childName] {
				reparentedSet[childName] = true
				reparentedChildren = append(reparentedChildren, childName)
			}
		}
	}

	// Apply every pre-reparent in one batch; divergence points are captured
	// before any mutation.
	if len(moves) > 0 {
		if err := eng.ReparentBranchesToParents(gctx, moves); err != nil {
			return nil, fmt.Errorf("failed to pre-reparent children: %w", err)
		}
	}

	return reparentedChildren, nil
}

func shouldPreserveDivergenceOnDelete(kind engine.DeletionReasonKind) bool {
	switch kind {
	case engine.DeletionReasonMergedPR, engine.DeletionReasonMergedIntoTrunk, engine.DeletionReasonEmptyWithPR:
		return true
	default:
		return false
	}
}

func mergeUniqueBranchNames(a []string, b []string) []string {
	if len(b) == 0 {
		return a
	}

	seen := make(map[string]bool, len(a)+len(b))
	out := make([]string, 0, len(a)+len(b))
	for _, name := range a {
		if !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	for _, name := range b {
		if !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}

	return out
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

		ctx.Output.Info("Removing worktree for deleted stack %s", output.BranchName(stackRoot))

		// Remove worktree directory if it exists
		if _, statErr := os.Stat(wt.Path.String()); statErr == nil {
			if removeErr := ctx.Engine.RemoveWorktree(ctx.Context, wt.Path.String()); removeErr != nil {
				ctx.Output.Debug("Failed to remove worktree at %s: %v", wt.Path, removeErr)
			}
		}

		// Unregister the worktree from the registry
		if unregErr := ctx.Engine.UnregisterWorktree(ctx.Context, stackRoot); unregErr != nil {
			ctx.Output.Debug("Failed to unregister worktree for %s: %v", stackRoot, unregErr)
		}
	}

	return mainRepoDir
}
