package abort

import (
	"fmt"

	"github.com/getstackit/stackit/internal/actions"
	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/config"
	"github.com/getstackit/stackit/internal/engine"
)

// Options contains options for the abort command
type Options struct {
	Force bool
}

// Action cancels an in-progress operation
func Action(ctx *app.Context, opts Options, handler Handler) error {
	if handler == nil {
		handler = &NullHandler{}
	}
	defer handler.Cleanup()

	eng := ctx.Engine
	out := ctx.Output

	rebaseInProgress := eng.IsRebaseInProgress(ctx.Context)
	mergeInProgress := eng.IsMergeInProgress(ctx.Context)

	// Check for continuation state
	hasContinuation := false
	if _, err := config.GetContinuationState(ctx.RepoRoot); err == nil {
		hasContinuation = true
	}

	if !rebaseInProgress && !mergeInProgress && !hasContinuation {
		out.Info("No operation in progress to abort.")
		return nil
	}

	// Confirm unless force is used
	if !opts.Force {
		confirmed, err := handler.PromptConfirmAbort()
		if err != nil {
			return fmt.Errorf("failed to get confirmation: %w", err)
		}
		if !confirmed {
			out.Info("Abort canceled.")
			return nil
		}
	}

	// Abort Git operations
	if rebaseInProgress {
		out.Info("Aborting rebase...")
		if err := eng.RebaseAbort(ctx.Context); err != nil {
			return fmt.Errorf("failed to abort rebase: %w", err)
		}
	}
	if mergeInProgress {
		out.Info("Aborting merge...")
		if err := eng.MergeAbort(ctx.Context); err != nil {
			return fmt.Errorf("failed to abort merge: %w", err)
		}
	}

	// Clear continuation state
	if hasContinuation {
		if err := config.ClearContinuationState(ctx.RepoRoot); err != nil {
			out.Debug("Failed to clear continuation state: %v", err)
		}
	}

	// Restore latest snapshot
	snapshots, err := eng.GetSnapshots()
	if err != nil {
		return fmt.Errorf("failed to get snapshots: %w", err)
	}

	if len(snapshots) > 0 {
		out.Info("Restoring to state before the command started...")
		// The latest snapshot should be the one taken before the command that halted
		if err := eng.RestoreSnapshot(ctx.Context, snapshots[0].ID); err != nil {
			return fmt.Errorf("failed to restore snapshot: %w", err)
		}
		restoreUncommittedWork(ctx, snapshots[0])
		actions.WarnIfLinearStackRestored(ctx, "Abort")
		out.Info("Successfully aborted and restored repository state.")
	} else {
		out.Info("Operation aborted. No undo history found to restore state.")
	}

	return nil
}

// restoreUncommittedWork hands back the uncommitted changes the halted command
// consumed. Restoring refs alone is not "where I started": modify amends the
// working tree into a commit before it restacks, so rolling that commit away
// without replacing the changes deletes work the user never committed
// themselves, leaving it reachable through nothing but the reflog.
//
// Best effort by design. The rollback has already succeeded at this point, and
// failing the whole abort over the working tree would strand the user mid-
// conflict; the failure names the ref the capture is anchored under instead, so
// the work is still reachable.
func restoreUncommittedWork(ctx *app.Context, snapshot engine.SnapshotInfo) {
	out := ctx.Output

	restored, err := ctx.Engine.RestoreWorktree(ctx.Context, snapshot.ID)
	if err != nil {
		out.Warn("Could not restore the uncommitted changes from before '%s': %v", snapshot.Command, err)
		return
	}
	if restored {
		out.Info("Restored the uncommitted changes you had before '%s'.", snapshot.Command)
	}
}
