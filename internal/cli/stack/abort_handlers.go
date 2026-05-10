package stack

import (
	"github.com/getstackit/stackit/internal/actions/abort"
	"github.com/getstackit/stackit/internal/cli/common"
	"github.com/getstackit/stackit/internal/output"
	"github.com/getstackit/stackit/internal/tui"
)

// NewAbortUI creates a handler for abort operations.
// Caller must defer handler.Cleanup() to restore terminal on exit.
func NewAbortUI(out output.Output, interactive bool) abort.Handler {
	if interactive {
		return NewInteractiveAbortHandler(out)
	}
	return NewSimpleAbortHandler(out)
}

// SimpleAbortHandler provides non-interactive handling for abort operations
type SimpleAbortHandler struct {
	common.BaseHandler
}

// NewSimpleAbortHandler creates a new SimpleAbortHandler
func NewSimpleAbortHandler(out output.Output) *SimpleAbortHandler {
	return &SimpleAbortHandler{
		BaseHandler: common.NewBaseHandler(out),
	}
}

// PromptConfirmAbort returns false for simple handler (non-interactive)
// In non-interactive mode, require --force flag to abort
func (h *SimpleAbortHandler) PromptConfirmAbort() (bool, error) {
	h.Output.Info("Use --force flag to abort without confirmation in non-interactive mode.")
	return false, nil
}

// InteractiveAbortHandler provides interactive prompts for abort operations
type InteractiveAbortHandler struct {
	SimpleAbortHandler
}

// NewInteractiveAbortHandler creates a new InteractiveAbortHandler
func NewInteractiveAbortHandler(out output.Output) *InteractiveAbortHandler {
	return &InteractiveAbortHandler{
		SimpleAbortHandler: *NewSimpleAbortHandler(out),
	}
}

// IsInteractive returns true for interactive handler
func (h *InteractiveAbortHandler) IsInteractive() bool {
	return true
}

// PromptConfirmAbort prompts user to confirm aborting the current operation
func (h *InteractiveAbortHandler) PromptConfirmAbort() (bool, error) {
	return tui.PromptConfirm("Are you sure you want to abort the current operation? This will roll back the repository to its previous state.", false)
}
