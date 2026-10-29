// Package squash implements the squash command, which squashes a branch's
// commits into one and restacks its upstack branches.
package squash

import (
	"context"
	"fmt"

	"github.com/getstackit/stackit/internal/actions"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/errors"
	"github.com/getstackit/stackit/internal/output"
)

// Options contains options for the squash command
type Options struct {
	// Branch is the branch to squash. Empty means the current branch. When it
	// names another branch, the action checks it out for the squash and
	// restores the original branch afterwards, on success or failure.
	Branch  string
	Message string
	NoEdit  bool
}

// Engine lists exactly the engine methods squash calls.
type Engine interface {
	actions.OwnershipEngine
	CurrentBranch(ctx context.Context) *engine.Branch
	CheckoutBranch(ctx context.Context, branch engine.Branch) error
	SquashCurrentBranch(ctx context.Context, opts engine.SquashOptions) error
	Graph(ctx context.Context, strategy engine.SortStrategy) *engine.StackGraph
}

// Deps are the collaborators squash needs. Adapters build them from their own
// runtime state; the action never sees bootstrap types.
type Deps struct {
	Engine Engine
	actions.MutationDeps
	Log output.Logger
	// Verify runs git commit hooks when set (the inverse of --no-verify).
	Verify bool
}

// Action performs the squash operation
func Action(ctx context.Context, deps Deps, opts Options) error {
	eng := deps.Engine
	out := deps.Out

	originalBranch := eng.CurrentBranch(ctx)
	currentBranch := originalBranch
	if opts.Branch != "" && (originalBranch == nil || originalBranch.Name() != opts.Branch) {
		target := eng.GetBranch(opts.Branch)
		currentBranch = &target
	}
	if currentBranch == nil {
		return errors.ErrNotOnBranch
	}

	if err := currentBranch.EnsureCanModify(); err != nil {
		return err
	}
	if err := actions.EnsureCanModify(ctx, eng, deps.Here, *currentBranch); err != nil {
		return err
	}

	if currentBranch != originalBranch {
		if err := eng.CheckoutBranch(ctx, *currentBranch); err != nil {
			return fmt.Errorf("checkout %s: %w", currentBranch.Name(), err)
		}
		if originalBranch != nil {
			defer func() {
				if err := eng.CheckoutBranch(ctx, *originalBranch); err != nil {
					deps.Log.Info("squash failed to restore branch=%v err=%v", originalBranch.Name(), err)
				}
			}()
		}
	}

	// Log entry point for diagnostics
	deps.Log.Info("squash started branch=%v", currentBranch.Name())

	// Take snapshot before modifying the repository
	snapshotOpts := actions.NewSnapshot("squash",
		actions.WithFlagValue("-m", opts.Message),
		actions.WithFlag(opts.NoEdit, "--no-edit"),
	)
	deps.Snapshots.Take(ctx, snapshotOpts)

	// Squash current branch
	if err := eng.SquashCurrentBranch(ctx, engine.SquashOptions{
		Message:  opts.Message,
		NoEdit:   opts.NoEdit,
		NoVerify: !deps.Verify,
	}); err != nil {
		return fmt.Errorf("failed to squash branch: %w", err)
	}

	out.Info("Squashed commits in %s.", output.CurrentBranch(currentBranch.Name()))
	deps.Log.Info("squash completed branch=%v", currentBranch.Name())

	// Get upstack branches (recursive children only, excluding current branch)
	rng := engine.StackRange{
		RecursiveParents:  false,
		IncludeCurrent:    false,
		RecursiveChildren: true,
	}
	graph := eng.Graph(ctx, engine.SortStrategyAlphabetical)
	upstackBranches := graph.Range(*currentBranch, rng)

	// Log upstack branches for diagnostics
	if len(upstackBranches) > 0 {
		upstackNames := make([]string, len(upstackBranches))
		for i, b := range upstackBranches {
			upstackNames[i] = b.Name()
		}
		deps.Log.Info("squash restacking upstack branches=%v count=%v", upstackNames, len(upstackBranches))
	} else {
		deps.Log.Info("squash no upstack branches to restack")
	}

	// Restack upstack branches
	if len(upstackBranches) > 0 {
		if err := deps.Restack.Restack(ctx, upstackBranches); err != nil {
			return fmt.Errorf("failed to restack upstack branches: %w", err)
		}
	}

	return nil
}
