package worktree

import (
	"fmt"

	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/output"
)

// AttachOptions contains options for the attach action.
type AttachOptions struct {
	Branch string // Any branch in the stack (we find the stack root)
	Name   string // Optional worktree name (defaults to stack root name)
}

// AttachAction creates a worktree for an existing stack.
func AttachAction(ctx *app.Context, opts AttachOptions) (*WorktreeResult, error) {
	eng := ctx.Engine
	out := ctx.Output
	if ctx.InManagedWorktree {
		return nil, fmt.Errorf("cannot attach from inside a managed worktree")
	}

	branch := eng.GetBranch(opts.Branch)
	if !branch.IsTracked() {
		if _, err := eng.GetRevision(branch); err != nil {
			return nil, fmt.Errorf("branch %s does not exist", output.BranchName(opts.Branch))
		}
		return nil, fmt.Errorf("branch %s is not tracked by stackit", output.BranchName(opts.Branch))
	}

	stackRootName := eng.GetStackRootForBranch(branch)
	if stackRootName == "" {
		return nil, fmt.Errorf("branch %s is not part of a stack (its parent must be trunk)", output.BranchName(opts.Branch))
	}
	stackRoot := eng.GetBranch(stackRootName)
	originalParent := eng.Trunk().GetName()
	if parent := stackRoot.GetParent(); parent != nil {
		originalParent = parent.GetName()
	}
	if eng.IsWorktreeAnchor(stackRoot) {
		return nil, fmt.Errorf("branch %s is already a worktree anchor", output.BranchName(stackRootName))
	}
	if existingWt, err := eng.GetWorktreeForStack(stackRootName); err != nil {
		return nil, fmt.Errorf("failed to check existing worktree: %w", err)
	} else if existingWt != nil {
		return nil, fmt.Errorf("stack already has a worktree at %s", existingWt.Path)
	}

	name := opts.Name
	if name == "" {
		name = stackRootName
	}
	currentBranch := eng.CurrentBranch()
	trunk := eng.Trunk()
	needsCheckout := currentBranch != nil && eng.GetStackRootForBranch(*currentBranch) == stackRootName
	if needsCheckout {
		if err := eng.CheckoutBranch(ctx.Context, trunk); err != nil {
			return nil, fmt.Errorf("failed to checkout trunk before creating worktree: %w", err)
		}
	}

	created, err := createAnchoredWorktree(ctx, eng, ctx.RepoRoot, anchoredWorktreeOptions{
		Name:           name,
		AnchorParent:   originalParent,
		RootBranch:     stackRootName,
		OriginalParent: originalParent,
	})
	if err != nil {
		if needsCheckout && currentBranch != nil {
			if restoreErr := eng.CheckoutBranch(ctx.Context, *currentBranch); restoreErr != nil {
				out.Warn("Could not return to %s: %v", output.BranchName(currentBranch.GetName()), restoreErr)
			}
		}
		return nil, err
	}

	out.Success("Attached stack %s to worktree", output.BranchName(stackRootName))
	out.Info("  Name: %s", output.BranchName(name))
	reportCreatedWorktree(ctx, created, "")
	return created, nil
}
