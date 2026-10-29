package fold

import (
	"context"
	"fmt"

	"github.com/getstackit/stackit/internal/actions/restack"

	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/output"
)

func foldWithKeep(ctx context.Context, restacker restack.Runner, currentBranch, parentBranch engine.Branch, eng Engine, splog output.Output, _ Options) error {
	// Build StackGraph for efficient traversals
	graph := eng.Graph(ctx, engine.SortStrategyAlphabetical)

	// Get all children of parent (siblings + current branch)
	allChildren := graph.ChildBranches(parentBranch)

	// Identify siblings (children of parent excluding current branch)
	siblings := engine.Branches{}
	for _, child := range allChildren {
		if child.Name() != currentBranch.Name() {
			siblings = siblings.Append(child)
		}
	}

	// Ensure we're on the current branch
	if err := eng.CheckoutBranch(ctx, currentBranch); err != nil {
		return fmt.Errorf("failed to checkout current branch: %w", err)
	}

	if err := fastForwardOrMerge(ctx, eng, parentBranch, currentBranch); err != nil {
		return err
	}

	// Delete the parent branch (engine reparents children to grandparent and
	// rebuilds internally).
	if err := eng.DeleteBranch(ctx, parentBranch); err != nil {
		return fmt.Errorf("failed to delete parent branch: %w", err)
	}

	// The current branch absorbed the parent via merge, so it needs a fresh
	// merge-base calculation against the grandparent (now its parent).
	// DeleteBranch preserved the old divergence point, which would drop the
	// merged commits at restack; DivergenceRecompute pulls them back in.
	refreshedCurrent := eng.GetBranch(currentBranch.Name())
	if grandparent := refreshedCurrent.Parent(); grandparent != nil {
		if err := eng.SetParent(ctx, refreshedCurrent, *grandparent, engine.DivergenceRecompute); err != nil {
			return fmt.Errorf("failed to reset divergence for %s: %w", currentBranch.Name(), err)
		}
	}

	// Reparent all siblings onto the current branch in one batch. Stack ID is
	// propagated automatically, and capturing divergence points up front keeps
	// related siblings correct.
	if len(siblings) > 0 {
		if err := eng.ReparentBranchesToParents(ctx, engine.MovesTo(siblings.Names(), refreshedCurrent.Name()), engine.ReparentOpts{}); err != nil {
			return fmt.Errorf("failed to reparent siblings to %s: %w", currentBranch.Name(), err)
		}
	}

	splog.Info("Folded %s into %s (kept %s).",
		output.CurrentBranch(parentBranch.Name()),
		output.BranchName(currentBranch.Name()),
		output.BranchName(currentBranch.Name()))

	// Rebuild graph with fresh engine state after deletion
	graph = eng.Graph(ctx, engine.SortStrategyAlphabetical)

	// Restack current branch and all its descendants
	branchesToRestack := graph.Range(currentBranch, engine.StackRange{
		RecursiveChildren: true,
		IncludeCurrent:    true,
		RecursiveParents:  false,
	})

	if err := restacker.Restack(ctx, branchesToRestack); err != nil {
		return fmt.Errorf("failed to restack branches: %w", err)
	}

	return nil
}
