package actions

import (
	"fmt"

	"stackit.dev/stackit/internal/app"
	"stackit.dev/stackit/internal/engine"
	"stackit.dev/stackit/internal/tui/style"
)

// SquashOptions contains options for the squash command
type SquashOptions struct {
	Message string
	NoEdit  bool
}

// SquashAction performs the squash operation
func SquashAction(ctx *app.Context, opts SquashOptions) error {
	eng := ctx.History()
	splog := ctx.Splog
	context := ctx.Context

	// Get current branch
	currentBranch := ctx.Navigator().CurrentBranch()
	if currentBranch == nil {
		return fmt.Errorf("not on a branch")
	}

	if err := currentBranch.EnsureCanModify(); err != nil {
		return err
	}

	// Take snapshot before modifying the repository
	snapshotOpts := NewSnapshot("squash",
		WithFlagValue("-m", opts.Message),
		WithFlag(opts.NoEdit, "--no-edit"),
	)
	if err := ctx.Undo().TakeSnapshot(snapshotOpts); err != nil {
		// Log but don't fail - snapshot is best effort
		splog.Debug("Failed to take snapshot: %v", err)
	}

	// Squash current branch
	if err := eng.SquashCurrentBranch(context, engine.SquashOptions{
		Message:  opts.Message,
		NoEdit:   opts.NoEdit,
		NoVerify: !ctx.Verify,
	}); err != nil {
		return fmt.Errorf("failed to squash branch: %w", err)
	}

	splog.Info("Squashed commits in %s.", style.ColorBranchName(currentBranch.GetName(), true))

	// Get upstack branches (recursive children only, excluding current branch)
	rng := engine.StackRange{
		RecursiveParents:  false,
		IncludeCurrent:    false,
		RecursiveChildren: true,
	}
	upstackBranches := currentBranch.GetRelativeStack(rng)

	// Restack upstack branches
	if len(upstackBranches) > 0 {
		if err := RestackBranches(ctx, upstackBranches); err != nil {
			return fmt.Errorf("failed to restack upstack branches: %w", err)
		}
	}

	return nil
}
