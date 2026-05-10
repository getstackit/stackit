// Package untrack implements the stackit untrack command for stopping branch tracking.
package untrack

import "github.com/getstackit/stackit/internal/actions/handler"

// Handler receives events from untrack action
type Handler interface {
	// PromptConfirmUntrackDescendants prompts user to confirm untracking descendants
	// Returns true to proceed with untracking all descendants, false to cancel
	PromptConfirmUntrackDescendants(branchName string, descendantCount int) (bool, error)

	// Cleanup restores terminal state (may be no-op)
	Cleanup()

	// IsInteractive returns true if the handler supports interactive prompts
	IsInteractive() bool
}

// NullHandler is a no-op handler for when nil is passed
type NullHandler struct {
	handler.NullBase
}

// PromptConfirmUntrackDescendants implements Handler. Returns false (cancel) for null handler.
// Use --force flag to untrack descendants without confirmation.
func (h *NullHandler) PromptConfirmUntrackDescendants(_ string, _ int) (bool, error) {
	return false, nil
}
