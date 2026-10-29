// Package pop implements the pop command: delete the current branch while
// keeping its changes staged on the parent.
package pop

import (
	"context"
	"fmt"

	"github.com/getstackit/stackit/internal/actions/validation"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/output"
)

// Engine lists exactly the engine methods pop calls.
type Engine interface {
	validation.GitOperationEngine
	SoftReset(ctx context.Context, revision string) error
	CheckoutBranch(ctx context.Context, branch engine.Branch) error
	DeleteBranch(ctx context.Context, branch engine.Branch) error
}

// Deps are the collaborators pop needs. Adapters build them from their own
// runtime state; the action never sees bootstrap types.
type Deps struct {
	Engine Engine
	Out    output.Output
}

// Action deletes the current branch but retains the state of files in the
// working tree.
func Action(ctx context.Context, deps Deps) error {
	eng, out := deps.Engine, deps.Out

	// Validate preconditions
	if err := (validation.Chain{
		validation.MustBeOnBranch(ctx, eng),
		validation.CurrentBranchMustNotBeTrunk(ctx, eng, "pop"),
		validation.CurrentBranchMustBeTracked(ctx, eng),
		validation.MustNotHaveRebaseInProgress(ctx, eng),
		validation.MustNotHaveUncommittedChanges(ctx, eng),
	}).Validate(); err != nil {
		return err
	}
	currentBranch := eng.CurrentBranch(ctx).Name()
	currentBranchObj := eng.GetBranch(currentBranch)

	// Get parent branch
	parentName := currentBranchObj.ParentOrTrunk()

	// Get parent branch revision
	parentBranch := eng.GetBranch(parentName)
	parentRev, err := parentBranch.GetRevision()
	if err != nil {
		return fmt.Errorf("failed to get parent revision: %w", err)
	}

	// Soft reset to parent - this uncommits the current branch's changes
	// and stages them, keeping the working tree unchanged
	if err := eng.SoftReset(ctx, parentRev); err != nil {
		return fmt.Errorf("failed to reset to parent: %w", err)
	}

	// Checkout parent branch
	if err := eng.CheckoutBranch(ctx, parentBranch); err != nil {
		return fmt.Errorf("failed to checkout parent branch: %w", err)
	}

	// Delete the old branch (this will also reparent any children)
	if err := eng.DeleteBranch(ctx, currentBranchObj); err != nil {
		return fmt.Errorf("failed to delete branch: %w", err)
	}

	// Check how many changes are staged
	hasStaged, err := eng.HasStagedChanges(ctx)
	if err == nil && hasStaged {
		out.Info("Popped branch %s. Changes are now staged on %s.",
			output.BranchName(currentBranch),
			output.BranchName(parentName))
	} else {
		out.Info("Popped branch %s. Switched to %s.",
			output.BranchName(currentBranch),
			output.BranchName(parentName))
	}

	return nil
}
