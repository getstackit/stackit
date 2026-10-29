package untrack

import (
	"context"
	"fmt"

	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/output"
)

// Options contains options for the untrack command
type Options struct {
	BranchName string
	Force      bool
}

// Engine lists exactly the engine methods untrack calls.
type Engine interface {
	engine.BranchLookup
	Graph(ctx context.Context, strategy engine.SortStrategy) *engine.StackGraph
	UntrackBranches(ctx context.Context, branchNames []string) error
}

// Deps are the collaborators untrack needs. Adapters build them from their
// own runtime state; the action never sees bootstrap types.
type Deps struct {
	Engine Engine
	Out    output.Output
}

// Action performs the untrack operation
func Action(ctx context.Context, deps Deps, opts Options, handler Handler) error {
	if handler == nil {
		handler = &NullHandler{}
	}
	defer handler.Cleanup()

	eng, out := deps.Engine, deps.Out
	branchName := opts.BranchName

	// Check if branch is tracked
	branch := eng.GetBranch(branchName)
	if !branch.IsTracked() {
		return fmt.Errorf("branch %s is not tracked", branchName)
	}

	// Find descendants
	graph := eng.Graph(ctx, engine.SortStrategyAlphabetical)
	descendants := graph.Range(branch, engine.StackRange{RecursiveChildren: true})

	// If there are descendants and not forced, prompt for confirmation
	if len(descendants) > 0 && !opts.Force {
		confirmed, err := handler.PromptConfirmUntrackDescendants(branchName, len(descendants))
		if err != nil {
			return err
		}

		if !confirmed {
			out.Info("Untrack canceled.")
			return nil
		}
	}

	// Collect all names to untrack (descendants first for display, then the branch itself).
	allNames := make([]string, 0, len(descendants)+1)
	for _, d := range descendants {
		allNames = append(allNames, d.Name())
	}
	allNames = append(allNames, branchName)

	// Delete all metadata refs in one batch and rebuild the engine once.
	if err := eng.UntrackBranches(ctx, allNames); err != nil {
		return fmt.Errorf("failed to untrack branches: %w", err)
	}

	for _, name := range allNames {
		out.Info("Stopped tracking %s.", output.BranchName(name))
	}

	return nil
}
