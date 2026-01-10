// Package undo provides functionality for undoing stackit operations using snapshots.
package undo

import (
	"fmt"

	"stackit.dev/stackit/internal/app"
	"stackit.dev/stackit/internal/engine"
	"stackit.dev/stackit/internal/timeutil"
)

// Options contains options for the undo command
type Options struct {
	SnapshotID string // Optional: specific snapshot to restore (skips interactive selection)
	Force      bool   // Optional: skip confirmation prompt
}

// Action performs the undo operation
func Action(ctx *app.Context, opts Options, handler Handler) error {
	eng := ctx.Engine

	// Use null handler if none provided
	if handler == nil {
		handler = &NullHandler{}
	}
	defer handler.Cleanup()

	handler.Start()

	// Get all available snapshots
	snapshots, err := eng.GetSnapshots()
	if err != nil {
		return fmt.Errorf("failed to get snapshots: %w", err)
	}

	if len(snapshots) == 0 {
		handler.Complete(true, "No undo history available.")
		return nil
	}

	handler.OnSnapshotList(snapshots)

	var selectedSnapshotID string

	// If snapshot ID is provided, use it directly
	if opts.SnapshotID != "" {
		// Verify the snapshot exists
		found := false
		for _, snap := range snapshots {
			if snap.ID == opts.SnapshotID {
				found = true
				selectedSnapshotID = snap.ID
				break
			}
		}
		if !found {
			return fmt.Errorf("snapshot %s not found", opts.SnapshotID)
		}
	} else {
		// Interactive selection
		switch {
		case len(snapshots) == 1:
			// Only one snapshot, use it directly
			selectedSnapshotID = snapshots[0].ID
			handler.OnStep(fmt.Sprintf("Restoring to: %s", snapshots[0].DisplayName), StepStarted)
		case handler.IsInteractive():
			// Multiple snapshots - use handler for interactive selection
			selected, err := handler.SelectSnapshot(snapshots)
			if err != nil {
				return fmt.Errorf("failed to select snapshot: %w", err)
			}
			selectedSnapshotID = selected
		default:
			// Non-interactive mode with multiple snapshots - use most recent
			selectedSnapshotID = snapshots[0].ID
			handler.OnStep(fmt.Sprintf("Using most recent snapshot: %s", snapshots[0].DisplayName), StepStarted)
		}
	}

	// Find the selected snapshot info for display
	var selectedSnapshot *engine.SnapshotInfo
	for _, snap := range snapshots {
		if snap.ID == selectedSnapshotID {
			selectedSnapshot = &snap
			break
		}
	}

	if selectedSnapshot == nil {
		return fmt.Errorf("selected snapshot not found")
	}

	// Count how many snapshots will be "undone" (all snapshots newer than the selected one)
	undoneCount := 0
	for _, snap := range snapshots {
		if snap.Timestamp.After(selectedSnapshot.Timestamp) {
			undoneCount++
		}
	}

	// Show confirmation prompt
	confirmMessage := fmt.Sprintf(
		"This will restore the repository to the state before '%s' (%s).",
		selectedSnapshot.Command,
		timeutil.FormatTimeAgo(selectedSnapshot.Timestamp),
	)
	if undoneCount > 0 {
		confirmMessage += fmt.Sprintf(" This will undo %d subsequent action(s).", undoneCount)
	}
	confirmMessage += " Are you sure?"

	confirmed := opts.Force
	if !opts.Force && handler.IsInteractive() {
		confirmed, err = handler.PromptConfirm(confirmMessage, false)
		if err != nil {
			return fmt.Errorf("failed to get confirmation: %w", err)
		}
	}

	if !confirmed {
		handler.Complete(true, "Undo canceled.")
		return nil
	}

	// Abort any in-progress Git operations that might interfere with restoration
	if eng.Git().IsRebaseInProgress(ctx.Context) {
		handler.OnStep("Aborting in-progress rebase before undo...", StepStarted)
		if err := eng.Git().RebaseAbort(ctx.Context); err != nil {
			return fmt.Errorf("failed to abort rebase: %w", err)
		}
		handler.OnStep("Aborted in-progress rebase", StepCompleted)
	}
	if eng.Git().IsMergeInProgress(ctx.Context) {
		handler.OnStep("Aborting in-progress merge before undo...", StepStarted)
		if err := eng.Git().MergeAbort(ctx.Context); err != nil {
			return fmt.Errorf("failed to abort merge: %w", err)
		}
		handler.OnStep("Aborted in-progress merge", StepCompleted)
	}

	// Perform the restoration
	handler.OnStep("Restoring repository state...", StepStarted)
	if err := eng.RestoreSnapshot(ctx.Context, selectedSnapshotID); err != nil {
		return fmt.Errorf("failed to restore snapshot: %w", err)
	}

	handler.Complete(true, fmt.Sprintf("Successfully restored to state before '%s'.", selectedSnapshot.Command))

	return nil
}
