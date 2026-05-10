// Package scope implements the stackit scope command for managing branch scopes.
package scope

import "github.com/getstackit/stackit/internal/actions/handler"

// Handler receives events from scope action
type Handler interface {
	// PromptConfirmRename prompts user to confirm branch rename after scope change
	// Returns true to proceed with rename, false to skip
	PromptConfirmRename(branchName, oldScope, newScope string) (bool, error)

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

// PromptConfirmRename implements Handler. Returns false (skip rename) for null handler.
func (h *NullHandler) PromptConfirmRename(string, string, string) (bool, error) { return false, nil }
