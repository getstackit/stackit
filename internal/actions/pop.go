package actions

import (
	"fmt"

	"github.com/getstackit/stackit/internal/actions/validation"
	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/tui/style"
)

// PopOptions contains options for the pop command
type PopOptions struct {
	// Currently no options, but structure is here for future extensibility
}

// PopAction deletes the current branch but retains the state of files in the working tree
func PopAction(ctx *app.Context, _ PopOptions) error {
	eng := ctx.Engine
	out := ctx.Output

	// Validate preconditions
	if err := (validation.Chain{
		validation.MustBeOnBranch(eng),
		validation.CurrentBranchMustNotBeTrunk(eng, "pop"),
		validation.CurrentBranchMustBeTracked(eng),
		validation.MustNotHaveRebaseInProgress(ctx.Context, ctx.Git()),
		validation.MustNotHaveUncommittedChanges(ctx.Context, ctx.Git()),
	}).Validate(); err != nil {
		return err
	}
	currentBranch := eng.CurrentBranch().GetName()
	currentBranchObj := eng.GetBranch(currentBranch)

	// Get parent branch
	parentName := currentBranchObj.GetParentOrTrunk()

	// Get parent branch revision
	parentBranch := eng.GetBranch(parentName)
	parentRev, err := parentBranch.GetRevision()
	if err != nil {
		return fmt.Errorf("failed to get parent revision: %w", err)
	}

	// Soft reset to parent - this uncommits the current branch's changes
	// and stages them, keeping the working tree unchanged
	if err := eng.Git().SoftReset(ctx.Context, parentRev); err != nil {
		return fmt.Errorf("failed to reset to parent: %w", err)
	}

	// Checkout parent branch
	if err := eng.CheckoutBranch(ctx.Context, parentBranch); err != nil {
		return fmt.Errorf("failed to checkout parent branch: %w", err)
	}

	// Delete the old branch (this will also reparent any children)
	if err := eng.DeleteBranch(ctx.Context, currentBranchObj); err != nil {
		return fmt.Errorf("failed to delete branch: %w", err)
	}

	// Check how many changes are staged
	hasStaged, err := eng.HasStagedChanges(ctx.Context)
	if err == nil && hasStaged {
		out.Info("Popped branch %s. Changes are now staged on %s.",
			style.ColorBranchName(currentBranch, false),
			style.ColorBranchName(parentName, false))
	} else {
		out.Info("Popped branch %s. Switched to %s.",
			style.ColorBranchName(currentBranch, false),
			style.ColorBranchName(parentName, false))
	}

	return nil
}
