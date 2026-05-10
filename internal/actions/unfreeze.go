package actions

import (
	"fmt"

	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/tui/style"
)

// UnfreezeAction unfreezes the specified branch and all branches upstack of it (recursive children)
func UnfreezeAction(ctx *app.Context, branchName string) error {
	eng := ctx.Engine
	out := ctx.Output

	branch := eng.GetBranch(branchName)
	if !branch.IsTracked() {
		return fmt.Errorf("branch %s is not tracked by stackit", branchName)
	}

	// Build StackGraph for efficient traversals
	graph := eng.Graph(engine.SortStrategyAlphabetical)

	// Get upstack (descendants including current)
	branches := graph.Range(branch, engine.StackRange{
		IncludeCurrent:    true,
		RecursiveChildren: true,
	})

	branchesToUnfreeze := []engine.Branch{}
	for _, b := range branches {
		if b.IsTrunk() {
			continue
		}
		branchesToUnfreeze = append(branchesToUnfreeze, b)
	}

	if len(branchesToUnfreeze) > 0 {
		res, err := eng.SetFrozen(ctx, branchesToUnfreeze, false)
		if err != nil {
			for name, branchErr := range res.Errors {
				out.Warn("Failed to unfreeze %s: %v", name, branchErr)
			}
			return fmt.Errorf("failed to unfreeze branches: %w", err)
		}

		for _, name := range res.AffectedBranches {
			out.Info("Unfrozen %s locally.", style.ColorBranchName(name, name == branchName))
		}
	}

	return nil
}
