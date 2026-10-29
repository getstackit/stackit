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
	// Branch is the branch to squash. Empty means the current branch. When it
	// names another branch, the action checks it out for the squash and
	// restores the original branch afterwards, on success or failure.
	Branch  string
	Message string
	NoEdit  bool
}

// SquashAction performs the squash operation
func SquashAction(ctx *app.Context, opts SquashOptions) error {
	eng := ctx.History()
	out := ctx.Output
	context := ctx.Context

	originalBranch := ctx.Navigator().CurrentBranch(ctx.Context)
	currentBranch := originalBranch
	if opts.Branch != "" && (originalBranch == nil || originalBranch.Name() != opts.Branch) {
		target := ctx.Navigator().GetBranch(opts.Branch)
		currentBranch = &target
	}
	if currentBranch == nil {
		return errors.ErrNotOnBranch
	}

	if err := currentBranch.EnsureCanModify(); err != nil {
		return err
	}
	if err := EnsureCanModifyHere(ctx, *currentBranch); err != nil {
		return err
	}

	if currentBranch != originalBranch {
		if err := ctx.Engine.CheckoutBranch(context, *currentBranch); err != nil {
			return fmt.Errorf("checkout %s: %w", currentBranch.Name(), err)
		}
		if originalBranch != nil {
			defer func() {
				if err := ctx.Engine.CheckoutBranch(context, *originalBranch); err != nil {
					ctx.Logger.Info("squash failed to restore branch=%v err=%v", originalBranch.Name(), err)
				}
			}()
		}
	}

	// Log entry point for diagnostics
	ctx.Logger.Info("squash started branch=%v", currentBranch.Name())

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

	out.Info("Squashed commits in %s.", output.CurrentBranch(currentBranch.Name()))
	ctx.Logger.Info("squash completed branch=%v", currentBranch.Name())

	// Get upstack branches (recursive children only, excluding current branch)
	rng := engine.StackRange{
		RecursiveParents:  false,
		IncludeCurrent:    false,
		RecursiveChildren: true,
	}
	graph := ctx.Engine.Graph(ctx.Context, engine.SortStrategyAlphabetical)
	upstackBranches := graph.Range(*currentBranch, rng)

	// Log upstack branches for diagnostics
	if len(upstackBranches) > 0 {
		upstackNames := make([]string, len(upstackBranches))
		for i, b := range upstackBranches {
			upstackNames[i] = b.Name()
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
