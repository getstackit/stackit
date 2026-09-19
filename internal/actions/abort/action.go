package abort

import (
	"fmt"

	"github.com/getstackit/stackit/internal/actions"
	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/config"
	"github.com/getstackit/stackit/internal/output"
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

	// Check for continuation state. It also names the snapshot the halted
	// command recorded, which is the only one abort may roll back to.
	continuation, continuationErr := config.GetContinuationState(ctx.RepoRoot)
	hasContinuation := continuationErr == nil

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

	return restoreBoundSnapshot(ctx, continuation)
}

// restoreBoundSnapshot rolls back to the snapshot the halted command recorded,
// and to no other.
//
// Abort used to restore whichever snapshot was newest on disk, on the
// assumption that it belonged to the halted command. For a command that took
// no snapshot — reorder, delete, submit and sync all used to — that assumption
// picked up some earlier command's snapshot instead: aborting a conflicted
// reorder would roll the repository back past a `create` and delete the branch
// that create had made. Restoring nothing is the only safe answer when the
// halted command left no rollback point.
func restoreBoundSnapshot(ctx *app.Context, continuation *config.ContinuationState) error {
	eng := ctx.Engine
	out := ctx.Output

	if continuation == nil || continuation.SnapshotID == "" {
		out.Info("Operation aborted. It recorded no rollback point, so branches are as it left them.")
		out.Info("Use %s to roll back to an earlier state.", output.Cyan("stackit undo"))
		return nil
	}

	snapshot, err := eng.LoadSnapshot(continuation.SnapshotID)
	if err != nil {
		// The snapshot aged out of the undo stack (undo.depth) or was removed
		// by hand. Guessing at a different one is what this function exists to
		// prevent.
		out.Warn("The rollback point recorded for this operation is no longer available.")
		out.Info("Operation aborted; branches are as it left them. Use %s to roll back to an earlier state.", output.Cyan("stackit undo"))
		return nil //nolint:nilerr // a missing snapshot is reported, not an abort failure
	}

	out.Info("Restoring to state before %s started...", output.Cyan("stackit "+snapshot.Command))
	if err := eng.RestoreSnapshot(ctx.Context, continuation.SnapshotID); err != nil {
		return fmt.Errorf("failed to restore snapshot: %w", err)
	}
	restoreUncommittedWork(ctx, continuation.SnapshotID, snapshot.Command)
	actions.WarnIfLinearStackRestored(ctx, "Abort")
	out.Info("Successfully aborted and restored repository state.")

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
func restoreUncommittedWork(ctx *app.Context, snapshotID, command string) {
	out := ctx.Output

	restored, err := ctx.Engine.RestoreWorktree(ctx.Context, snapshotID)
	if err != nil {
		out.Warn("Could not restore the uncommitted changes from before '%s': %v", command, err)
		return
	}
	if restored {
		out.Info("Restored the uncommitted changes you had before '%s'.", command)
	}
}
