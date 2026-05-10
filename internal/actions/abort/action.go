package abort

import (
	"fmt"

	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/config"
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

	rebaseInProgress := eng.Git().IsRebaseInProgress(ctx.Context)
	mergeInProgress := eng.Git().IsMergeInProgress(ctx.Context)

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
		if err := eng.Git().RebaseAbort(ctx.Context); err != nil {
			return fmt.Errorf("failed to abort rebase: %w", err)
		}
	}
	if mergeInProgress {
		out.Info("Aborting merge...")
		if err := eng.Git().MergeAbort(ctx.Context); err != nil {
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
		out.Info("Successfully aborted and restored repository state.")
	} else {
		out.Info("Operation aborted. No undo history found to restore state.")
	}

	return nil
}
