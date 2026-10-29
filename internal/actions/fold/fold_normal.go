package fold

import (
	"context"
	"fmt"

	"github.com/getstackit/stackit/internal/actions/restack"

	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/output"
)

func foldNormal(ctx context.Context, restacker restack.Runner, currentBranch, parentBranch engine.Branch, eng Engine, splog output.Output, _ Options) error {
	// Checkout parent branch (engine updates its currentBranch internally).
	if err := eng.CheckoutBranch(ctx, parentBranch); err != nil {
		return fmt.Errorf("failed to checkout parent branch: %w", err)
	}

	if err := fastForwardOrMerge(ctx, eng, currentBranch, parentBranch); err != nil {
		return err
	}

	// Build StackGraph for traversals
	graph := eng.Graph(ctx, engine.SortStrategyAlphabetical)

	// Get all descendants of parent before deletion (for restacking)
	descendants := graph.Range(parentBranch, engine.StackRange{
		RecursiveChildren: true,
		IncludeCurrent:    false,
		RecursiveParents:  false,
	})

	// Delete the current branch (this will automatically reparent its children to parent)
	if err := eng.DeleteBranch(ctx, currentBranch); err != nil {
		return fmt.Errorf("failed to delete branch: %w", err)
	}

	splog.Info("Folded %s into %s.",
		output.CurrentBranch(currentBranch.Name()),
		output.BranchName(parentBranch.Name()))

	// Restack all descendants of the parent
	if len(descendants) > 0 {
		// DeleteBranch rebuilds engine state internally; just refresh the graph
		// snapshot (graphs are immutable copies of engine state at query time).
		graph = eng.Graph(ctx, engine.SortStrategyAlphabetical)

		// Get updated descendants list (current branch's children are now children of parent)
		updatedDescendants := graph.Range(parentBranch, engine.StackRange{
			RecursiveChildren: true,
			IncludeCurrent:    false,
			RecursiveParents:  false,
		})

		if err := restacker.Restack(ctx, updatedDescendants); err != nil {
			return fmt.Errorf("failed to restack branches: %w", err)
		}
	}

	return nil
}

// fastForwardOrMerge merges source into the checked-out target branch,
// fast-forwarding when possible and falling back to a merge commit.
func fastForwardOrMerge(ctx context.Context, eng Engine, source, target engine.Branch) error {
	if err := eng.Merge(ctx, source.Name(), engine.MergeOptions{FFOnly: true}); err == nil {
		return nil
	}
	if err := eng.Merge(ctx, source.Name(), engine.MergeOptions{NoEdit: true}); err != nil {
		return fmt.Errorf("failed to merge %s into %s due to conflicts. Please resolve the conflicts and run 'git commit', or abort with 'git merge --abort'", source.Name(), target.Name())
	}
	return nil
}
