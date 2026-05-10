// Package undo provides functionality for undoing stackit operations using snapshots.
package undo

import (
	"fmt"

	"github.com/getstackit/stackit/internal/actions/handler"
	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/timeutil"
)

// Options contains options for the undo command
type Options struct {
	SnapshotID string // Optional: specific snapshot to restore (skips interactive selection)
	Force      bool   // Optional: skip confirmation prompt
}

// Action performs the undo operation
func Action(ctx *app.Context, opts Options, h Handler) error {
	eng := ctx.Engine

	// Use null handler if none provided
	if h == nil {
		h = &NullHandler{}
	}
	defer h.Cleanup()

	h.Start()

	// Get all available snapshots
	snapshots, err := eng.GetSnapshots()
	if err != nil {
		return fmt.Errorf("failed to get snapshots: %w", err)
	}

	if len(snapshots) == 0 {
		h.Complete(true, "No undo history available.")
		return nil
	}

	h.OnSnapshotList(snapshots)

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
			h.OnStep(fmt.Sprintf("Restoring to: %s", snapshots[0].DisplayName), handler.StatusStarted)
		case h.IsInteractive():
			// Multiple snapshots - use handler for interactive selection
			selected, err := h.SelectSnapshot(snapshots)
			if err != nil {
				return fmt.Errorf("failed to select snapshot: %w", err)
			}
			selectedSnapshotID = selected
		default:
			// Non-interactive mode with multiple snapshots - use most recent
			selectedSnapshotID = snapshots[0].ID
			h.OnStep(fmt.Sprintf("Using most recent snapshot: %s", snapshots[0].DisplayName), handler.StatusStarted)
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
	if !opts.Force && h.IsInteractive() {
		confirmed, err = h.PromptConfirm(confirmMessage, false)
		if err != nil {
			return fmt.Errorf("failed to get confirmation: %w", err)
		}
	}

	if !confirmed {
		h.Complete(true, "Undo canceled.")
		return nil
	}

	// Abort any in-progress Git operations that might interfere with restoration
	if eng.Git().IsRebaseInProgress(ctx.Context) {
		h.OnStep("Aborting in-progress rebase before undo...", handler.StatusStarted)
		if err := eng.Git().RebaseAbort(ctx.Context); err != nil {
			return fmt.Errorf("failed to abort rebase: %w", err)
		}
		h.OnStep("Aborted in-progress rebase", handler.StatusCompleted)
	}
	if eng.Git().IsMergeInProgress(ctx.Context) {
		h.OnStep("Aborting in-progress merge before undo...", handler.StatusStarted)
		if err := eng.Git().MergeAbort(ctx.Context); err != nil {
			return fmt.Errorf("failed to abort merge: %w", err)
		}
		h.OnStep("Aborted in-progress merge", handler.StatusCompleted)
	}

	// Perform the restoration
	h.OnStep("Restoring repository state...", handler.StatusStarted)
	if err := eng.RestoreSnapshot(ctx.Context, selectedSnapshotID); err != nil {
		return fmt.Errorf("failed to restore snapshot: %w", err)
	}

	h.Complete(true, fmt.Sprintf("Successfully restored to state before '%s'.", selectedSnapshot.Command))

	return nil
}
