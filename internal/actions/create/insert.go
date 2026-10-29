package create

import (
	"context"
	"fmt"
	"slices"

	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/output"
)

func handleInsert(ctx context.Context, eng Engine, out output.Output, newBranch, currentBranch string, opts *Options, h Handler) error {
	// Build StackGraph for efficient traversals
	graph := eng.Graph(ctx, engine.SortStrategyAlphabetical)

	children := graph.ChildBranches(eng.GetBranch(currentBranch))
	siblings := []string{}
	for _, child := range children {
		if child.Name() != newBranch {
			siblings = append(siblings, child.Name())
		}
	}

	if len(siblings) == 0 {
		return nil
	}

	// If multiple children, prompt user to select which to move
	var toMove []string
	switch {
	case len(opts.SelectedChildren) > 0:
		// Use pre-selected children (for tests)
		for _, selected := range opts.SelectedChildren {
			if slices.Contains(siblings, selected) {
				toMove = append(toMove, selected)
			}
		}
	case len(siblings) > 1 && h.IsInteractive():
		out.Info("Current branch has multiple children. Select which should be moved onto the new branch:")
		selected, err := h.PromptChildToMove(siblings)
		if err != nil {
			return err
		}

		if selected == "all" {
			toMove = siblings
		} else {
			toMove = []string{selected}
		}
	default:
		// Single child or non-interactive - move all
		toMove = siblings
	}

	// Reparent every moving child onto the new branch in one batch, recomputing
	// each child's divergence against it.
	if err := eng.ReparentBranchesToParents(ctx, engine.MovesTo(toMove, newBranch), engine.ReparentOpts{Divergence: engine.DivergenceRecompute}); err != nil {
		return fmt.Errorf("failed to update parents onto %s: %w", newBranch, err)
	}

	allToRestack := engine.NewBranchesBuilder(len(toMove) * 2)
	for _, child := range toMove {
		childBranch := eng.GetBranch(child)
		allToRestack.Add(childBranch)

		// Include all descendants in the restack operation
		allToRestack.AddAll(graph.Range(childBranch, engine.StackRange{RecursiveChildren: true}))
	}

	// Sort topologically to ensure we restack from bottom to top
	branchesToRestack := eng.SortBranchesTopologically(ctx, allToRestack.Build())

	// Restack children onto the new branch to physically insert it
	if len(branchesToRestack) > 0 {
		batchRes, err := eng.RestackBranches(ctx, branchesToRestack, engine.RestackOpts{})
		if err != nil {
			out.Info("Warning: failed to restack branches onto %s: %v", newBranch, err)
		}

		for _, branch := range branchesToRestack {
			child := branch.Name()
			res, ok := batchRes.Results[child]
			if !ok {
				continue
			}

			switch res.Result {
			case engine.RestackConflict:
				out.Info("Conflict restacking %s onto %s. Please resolve manually or run 'stackit sync --restack'.", child, newBranch)
			case engine.RestackDone:
				out.Info("Restacked %s onto %s.", child, newBranch)
			}
		}
	}

	return nil
}
