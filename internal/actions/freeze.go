package actions

import (
	"fmt"

	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/tui/style"
)

// FreezeAction freezes the specified branch and all branches downstack of it (recursive parents)
func FreezeAction(ctx *app.Context, branchName string) error {
	eng := ctx.Engine
	out := ctx.Output

	branch := eng.GetBranch(branchName)
	if branch.IsTrunk() {
		return fmt.Errorf("cannot freeze trunk branch %s", branchName)
	}

	if !branch.IsTracked() {
		return fmt.Errorf("branch %s is not tracked by stackit", branchName)
	}

	// Build StackGraph for efficient traversals
	graph := eng.Graph(engine.SortStrategyAlphabetical)

	// Get downstack (ancestors including current)
	branches := graph.Range(branch, engine.StackRange{
		RecursiveParents: true,
		IncludeCurrent:   true,
	})

	branchesToFreeze := []engine.Branch{}
	for _, b := range branches {
		if b.IsTrunk() {
			continue
		}
		branchesToFreeze = append(branchesToFreeze, b)
	}

	if len(branchesToFreeze) > 0 {
		res, err := eng.SetFrozen(ctx, branchesToFreeze, true)
		if err != nil {
			for name, branchErr := range res.Errors {
				out.Warn("Failed to freeze %s: %v", name, branchErr)
			}
			return fmt.Errorf("failed to freeze branches: %w", err)
		}

		for _, name := range res.AffectedBranches {
			out.Info("Frozen %s locally.", style.ColorBranchName(name, name == branchName))
		}
	}

	return nil
}
