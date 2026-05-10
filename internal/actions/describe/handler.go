// Package describe implements the stackit describe command for managing stack descriptions.
package describe

import "github.com/getstackit/stackit/internal/actions/handler"

// Handler receives events from describe action
type Handler interface {
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
