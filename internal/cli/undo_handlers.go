package cli

import (
	"github.com/getstackit/stackit/internal/actions/handler"
	"github.com/getstackit/stackit/internal/actions/undo"
	"github.com/getstackit/stackit/internal/cli/common"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/output"
	"github.com/getstackit/stackit/internal/tui"
)

// NewUndoUI creates a runner and handler pair for undo operations.
// The runner manages terminal state; the handler processes events.
// Caller must defer runner.Cleanup() to restore terminal on exit.
// Currently returns nil runner as interactive prompts are handled inline.
func NewUndoUI(out output.Output, _ output.Logger) (*tui.Runner, undo.Handler) {
	if tui.IsTTY() {
		return nil, NewInteractiveUndoHandler(out)
	}
	return nil, NewSimpleUndoHandler(out)
}

// SimpleUndoHandler provides streaming text output for undo operations
type SimpleUndoHandler struct {
	common.BaseHandler
}

// NewSimpleUndoHandler creates a new SimpleUndoHandler
func NewSimpleUndoHandler(out output.Output) *SimpleUndoHandler {
	return &SimpleUndoHandler{
		BaseHandler: common.NewBaseHandler(out),
	}
}

// Start is called at the beginning of undo
func (h *SimpleUndoHandler) Start() {}

// OnSnapshotList is called with available snapshots
func (h *SimpleUndoHandler) OnSnapshotList(_ []engine.SnapshotInfo) {}

// OnStep is called for each undo step
func (h *SimpleUndoHandler) OnStep(description string, status handler.StepStatus) {
	h.Lock()
	defer h.Unlock()

	switch status {
	case handler.StatusStarted:
		h.Output.Info("%s", description)
	case handler.StatusCompleted:
		h.Output.Info("  ✓ %s", description)
	case handler.StatusSkipped:
		h.Output.Info("  - %s (skipped)", description)
	}
}

// Complete is called when undo finishes
func (h *SimpleUndoHandler) Complete(_ bool, message string) {
	h.Lock()
	defer h.Unlock()

	h.Output.Info("%s", message)
}

// SelectSnapshot is not supported in non-interactive mode
func (h *SimpleUndoHandler) SelectSnapshot(_ []engine.SnapshotInfo) (string, error) {
	return "", nil
}

// PromptConfirm is not supported in non-interactive mode
func (h *SimpleUndoHandler) PromptConfirm(_ string, _ bool) (bool, error) {
	return false, nil
}

// InteractiveUndoHandler provides interactive TUI for undo operations
type InteractiveUndoHandler struct {
	common.BaseHandler
}

// NewInteractiveUndoHandler creates a new InteractiveUndoHandler
func NewInteractiveUndoHandler(out output.Output) *InteractiveUndoHandler {
	return &InteractiveUndoHandler{
		BaseHandler: common.NewBaseHandler(out),
	}
}

// Start is called at the beginning of undo
func (h *InteractiveUndoHandler) Start() {}

// OnSnapshotList is called with available snapshots
func (h *InteractiveUndoHandler) OnSnapshotList(_ []engine.SnapshotInfo) {}

// OnStep is called for each undo step
func (h *InteractiveUndoHandler) OnStep(description string, status handler.StepStatus) {
	h.Lock()
	defer h.Unlock()

	switch status {
	case handler.StatusStarted:
		h.Output.Info("%s", description)
	case handler.StatusCompleted:
		h.Output.Info("  ✓ %s", description)
	case handler.StatusSkipped:
		h.Output.Info("  - %s (skipped)", description)
	}
}

// Complete is called when undo finishes
func (h *InteractiveUndoHandler) Complete(_ bool, message string) {
	h.Lock()
	defer h.Unlock()

	h.Output.Info("%s", message)
}

// IsInteractive returns true for the interactive handler
func (h *InteractiveUndoHandler) IsInteractive() bool { return true }

// SelectSnapshot prompts user to select a snapshot
func (h *InteractiveUndoHandler) SelectSnapshot(snapshots []engine.SnapshotInfo) (string, error) {
	options := make([]tui.SelectOption, len(snapshots))
	for i, snap := range snapshots {
		options[i] = tui.SelectOption{
			Label: snap.DisplayName,
			Value: snap.ID,
		}
	}

	return tui.PromptSelect("Select state to restore:", options, 0)
}

// PromptConfirm prompts user for confirmation
func (h *InteractiveUndoHandler) PromptConfirm(message string, defaultYes bool) (bool, error) {
	return tui.PromptConfirm(message, defaultYes)
}
