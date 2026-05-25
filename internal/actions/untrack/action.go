package untrack

import (
	"fmt"

	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/tui/style"
)

// Options contains options for the untrack command
type Options struct {
	BranchName string
	Force      bool
}

// Action performs the untrack operation
func Action(ctx *app.Context, opts Options, handler Handler) error {
	if handler == nil {
		handler = &NullHandler{}
	}
	defer handler.Cleanup()

	eng := ctx.Engine
	branchName := opts.BranchName

	// Check if branch is tracked
	branch := eng.GetBranch(branchName)
	if !branch.IsTracked() {
		return fmt.Errorf("branch %s is not tracked", branchName)
	}

	// Find descendants
	graph := eng.Graph(engine.SortStrategyAlphabetical)
	descendants := graph.Range(branch, engine.StackRange{RecursiveChildren: true})

	// If there are descendants and not forced, prompt for confirmation
	if len(descendants) > 0 && !opts.Force {
		confirmed, err := handler.PromptConfirmUntrackDescendants(branchName, len(descendants))
		if err != nil {
			return err
		}

		if !confirmed {
			ctx.Output.Info("Untrack canceled.")
			return nil
		}
	}

	// Untrack recursively (descendants first, then the branch itself)
	// Actually order doesn't strictly matter for metadata deletion but it's cleaner
	for _, descendant := range descendants {
		if err := eng.UntrackBranch(ctx.Context, descendant.GetName()); err != nil {
			return fmt.Errorf("failed to untrack descendant %s: %w", descendant.GetName(), err)
		}
		ctx.Output.Info("Stopped tracking %s.", style.ColorBranchName(descendant.GetName(), false))
	}

	if err := eng.UntrackBranch(ctx.Context, branchName); err != nil {
		return fmt.Errorf("failed to untrack branch %s: %w", branchName, err)
	}
	ctx.Output.Info("Stopped tracking %s.", style.ColorBranchName(branchName, false))

	return nil
}
