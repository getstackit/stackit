package create

import (
	"context"
	"fmt"

	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/tui"
	"github.com/getstackit/stackit/internal/utils"
)

func handleInsert(ctx context.Context, newBranch, currentBranch string, runtimeCtx *app.Context, opts *Options) error {
	// Build StackGraph for efficient traversals
	graph := runtimeCtx.Engine.Graph(engine.SortStrategyAlphabetical)

	children := graph.ChildBranches(runtimeCtx.Engine.GetBranch(currentBranch))
	siblings := []string{}
	for _, child := range children {
		if child.GetName() != newBranch {
			siblings = append(siblings, child.GetName())
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
			for _, sibling := range siblings {
				if selected == sibling {
					toMove = append(toMove, sibling)
					break
				}
			}
		}
	case len(siblings) > 1 && utils.IsInteractive():
		runtimeCtx.Output.Info("Current branch has multiple children. Select which should be moved onto the new branch:")
		options := []tui.SelectOption{
			{Label: "All children", Value: "all"},
		}
		for _, child := range siblings {
			options = append(options, tui.SelectOption{Label: child, Value: child})
		}

		selected, err := tui.PromptSelect("Which child should be moved onto the new branch?", options, 0)
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
	if err := runtimeCtx.Engine.ReparentBranchesRecompute(ctx, toMove, runtimeCtx.Engine.GetBranch(newBranch)); err != nil {
		return fmt.Errorf("failed to update parents onto %s: %w", newBranch, err)
	}

	allToRestack := engine.NewBranchesBuilder(len(toMove) * 2)
	for _, child := range toMove {
		childBranch := runtimeCtx.Engine.GetBranch(child)
		allToRestack.Add(childBranch)

		// Include all descendants in the restack operation
		allToRestack.AddAll(graph.Range(childBranch, engine.StackRange{RecursiveChildren: true}))
	}

	// Sort topologically to ensure we restack from bottom to top
	branchesToRestack := runtimeCtx.Engine.SortBranchesTopologically(allToRestack.Build())

	// Restack children onto the new branch to physically insert it
	if len(branchesToRestack) > 0 {
		batchRes, err := runtimeCtx.Engine.RestackBranches(ctx, branchesToRestack)
		if err != nil {
			runtimeCtx.Output.Info("Warning: failed to restack branches onto %s: %v", newBranch, err)
		}

		for _, branch := range branchesToRestack {
			child := branch.GetName()
			res, ok := batchRes.Results[child]
			if !ok {
				continue
			}

			switch res.Result {
			case engine.RestackConflict:
				runtimeCtx.Output.Info("Conflict restacking %s onto %s. Please resolve manually or run 'stackit sync --restack'.", child, newBranch)
			case engine.RestackDone:
				runtimeCtx.Output.Info("Restacked %s onto %s.", child, newBranch)
			}
		}
	}

	return nil
}
