// Package abort implements the stackit abort command for canceling in-progress operations.
package abort

import "github.com/getstackit/stackit/internal/actions/handler"

// Handler receives events from abort action
type Handler interface {
	// PromptConfirmAbort prompts user to confirm aborting the current operation
	// Returns true to proceed with abort, false to cancel
	PromptConfirmAbort() (bool, error)

	// Cleanup restores terminal state (may be no-op)
	Cleanup()

	// IsInteractive returns true if the handler supports interactive prompts
	IsInteractive() bool
}

// NullHandler is a no-op handler for when nil is passed
type NullHandler struct {
	handler.NullBase
}

// PromptConfirmAbort implements Handler. Returns false (cancel) for null handler.
func (h *NullHandler) PromptConfirmAbort() (bool, error) { return false, nil }
