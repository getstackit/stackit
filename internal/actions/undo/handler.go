package undo

import (
	"stackit.dev/stackit/internal/engine"
)

// StepStatus represents the status of an undo step
type StepStatus string

// Step status constants
const (
	StepStarted   StepStatus = "started"
	StepCompleted StepStatus = "completed"
	StepSkipped   StepStatus = "skipped"
)

// Handler receives events from undo action
type Handler interface {
	// Start is called at the beginning of undo
	Start()

	// OnSnapshotList is called with available snapshots
	OnSnapshotList(snapshots []engine.SnapshotInfo)

	// OnStep is called for each undo step
	OnStep(description string, status StepStatus)

	// Complete is called when undo finishes
	Complete(success bool, message string)

	// Cleanup restores terminal state (may be no-op)
	Cleanup()

	// IsInteractive returns true if the handler supports interactive prompts
	IsInteractive() bool

	// SelectSnapshot prompts user to select a snapshot (interactive only)
	SelectSnapshot(snapshots []engine.SnapshotInfo) (string, error)

	// PromptConfirm prompts user for confirmation (interactive only)
	PromptConfirm(message string, defaultYes bool) (bool, error)
}

// NullHandler is a no-op handler for when nil is passed
type NullHandler struct{}

// Start implements Handler.
func (h *NullHandler) Start() {}

// OnSnapshotList implements Handler.
func (h *NullHandler) OnSnapshotList(_ []engine.SnapshotInfo) {}

// OnStep implements Handler.
func (h *NullHandler) OnStep(_ string, _ StepStatus) {}

// Complete implements Handler.
func (h *NullHandler) Complete(_ bool, _ string) {}

// Cleanup implements Handler.
func (h *NullHandler) Cleanup() {}

// IsInteractive implements Handler.
func (h *NullHandler) IsInteractive() bool { return false }

// SelectSnapshot implements Handler.
func (h *NullHandler) SelectSnapshot(_ []engine.SnapshotInfo) (string, error) { return "", nil }

// PromptConfirm implements Handler.
func (h *NullHandler) PromptConfirm(_ string, _ bool) (bool, error) { return false, nil }
