package fold

import (
	"context"
	"fmt"

	"github.com/getstackit/stackit/internal/actions"
	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/output"
)

func foldNormal(gctx context.Context, ctx *app.Context, currentBranch, parentBranch engine.Branch, eng foldEngine, splog output.Output, _ Options) error {
	// Checkout parent branch (engine updates its currentBranch internally).
	if err := eng.CheckoutBranch(gctx, parentBranch); err != nil {
		return fmt.Errorf("failed to checkout parent branch: %w", err)
	}

	if err := fastForwardOrMerge(gctx, eng, currentBranch, parentBranch); err != nil {
		return err
	}

	// Build StackGraph for traversals
	graph := eng.Graph(engine.SortStrategyAlphabetical)

	// Get all descendants of parent before deletion (for restacking)
	descendants := graph.Range(parentBranch, engine.StackRange{
		RecursiveChildren: true,
		IncludeCurrent:    false,
		RecursiveParents:  false,
	})

	// Delete the current branch (this will automatically reparent its children to parent)
	if err := eng.DeleteBranch(gctx, currentBranch); err != nil {
		return fmt.Errorf("failed to delete branch: %w", err)
	}

	splog.Info("Folded %s into %s.",
		output.CurrentBranch(currentBranch.GetName()),
		output.BranchName(parentBranch.GetName()))

	// Restack all descendants of the parent
	if len(descendants) > 0 {
		// DeleteBranch rebuilds engine state internally; just refresh the graph
		// snapshot (graphs are immutable copies of engine state at query time).
		graph = eng.Graph(engine.SortStrategyAlphabetical)

		// Get updated descendants list (current branch's children are now children of parent)
		updatedDescendants := graph.Range(parentBranch, engine.StackRange{
			RecursiveChildren: true,
			IncludeCurrent:    false,
			RecursiveParents:  false,
		})

		if err := actions.RestackBranches(ctx, updatedDescendants); err != nil {
			return fmt.Errorf("failed to restack branches: %w", err)
		}
	}

	return nil
}

// fastForwardOrMerge merges source into the checked-out target branch,
// fast-forwarding when possible and falling back to a merge commit.
func fastForwardOrMerge(gctx context.Context, eng foldEngine, source, target engine.Branch) error {
	if err := eng.Merge(gctx, source.GetName(), engine.MergeOptions{FFOnly: true}); err == nil {
		return nil
	}
	if err := eng.Merge(gctx, source.GetName(), engine.MergeOptions{NoEdit: true}); err != nil {
		return fmt.Errorf("failed to merge %s into %s due to conflicts. Please resolve the conflicts and run 'git commit', or abort with 'git merge --abort'", source.GetName(), target.GetName())
	}
	return nil
}
