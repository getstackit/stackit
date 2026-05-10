// Package navigation implements the stackit top/bottom commands for navigating stacked branches.
package navigation

import "github.com/getstackit/stackit/internal/actions/handler"

// Handler receives events from navigation action
type Handler interface {
	// PromptSelectBranch prompts user to select a branch when multiple children exist
	// Returns the selected branch name
	PromptSelectBranch(message string, branches []string) (string, error)

	// Cleanup restores terminal state (may be no-op)
	Cleanup()

	// IsInteractive returns true if the handler supports interactive prompts
	IsInteractive() bool
}

// NullHandler is a no-op handler for when nil is passed.
// It embeds handler.NullBase for Cleanup() and IsInteractive().
type NullHandler struct {
	handler.NullBase
}

// PromptSelectBranch implements Handler. Returns empty string for null handler.
func (h *NullHandler) PromptSelectBranch(string, []string) (string, error) { return "", nil }
