package actions

import (
	"fmt"

	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/errors"
	"github.com/getstackit/stackit/internal/output"
)

// SquashOptions contains options for the squash command
type SquashOptions struct {
	Message string
	NoEdit  bool
}

// SquashAction performs the squash operation
func SquashAction(ctx *app.Context, opts SquashOptions) error {
	eng := ctx.History()
	out := ctx.Output
	context := ctx.Context

	// Get current branch
	currentBranch := ctx.Navigator().CurrentBranch()
	if currentBranch == nil {
		return errors.ErrNotOnBranch
	}

	if err := currentBranch.EnsureCanModify(); err != nil {
		return err
	}

	// Log entry point for diagnostics
	ctx.Logger.Info("squash started branch=%v", currentBranch.GetName())

	// Take snapshot before modifying the repository
	snapshotOpts := NewSnapshot("squash",
		WithFlagValue("-m", opts.Message),
		WithFlag(opts.NoEdit, "--no-edit"),
	)
	TakeBestEffortSnapshot(ctx, snapshotOpts)

	// Squash current branch
	if err := eng.SquashCurrentBranch(context, engine.SquashOptions{
		Message:  opts.Message,
		NoEdit:   opts.NoEdit,
		NoVerify: !ctx.Verify,
	}); err != nil {
		return fmt.Errorf("failed to squash branch: %w", err)
	}

	out.Info("Squashed commits in %s.", output.Branch(currentBranch.GetName(), true))
	ctx.Logger.Info("squash completed branch=%v", currentBranch.GetName())

	// Get upstack branches (recursive children only, excluding current branch)
	rng := engine.StackRange{
		RecursiveParents:  false,
		IncludeCurrent:    false,
		RecursiveChildren: true,
	}
	graph := ctx.Engine.Graph(engine.SortStrategyAlphabetical)
	upstackBranches := graph.Range(*currentBranch, rng)

	// Log upstack branches for diagnostics
	if len(upstackBranches) > 0 {
		upstackNames := make([]string, len(upstackBranches))
		for i, b := range upstackBranches {
			upstackNames[i] = b.GetName()
		}
		ctx.Logger.Info("squash restacking upstack branches=%v count=%v", upstackNames, len(upstackBranches))
	} else {
		ctx.Logger.Info("squash no upstack branches to restack")
	}

	// Restack upstack branches
	if len(upstackBranches) > 0 {
		if err := RestackBranches(ctx, upstackBranches); err != nil {
			return fmt.Errorf("failed to restack upstack branches: %w", err)
		}
	}

	return nil
}
