package delete

import "github.com/getstackit/stackit/internal/actions/handler"

// Result contains the result of the delete action.
type Result struct {
	MainRepoDirForSwitch string
}

// Status represents the status of a branch deletion
type Status string

// Delete status constants
const (
	StatusDeleted   Status = "deleted"
	StatusSkipped   Status = "skipped"
	StatusRestacked Status = "restacked"
)

// Handler receives events from delete action
type Handler interface {
	// Start is called at the beginning of delete
	Start(branchCount int)

	// OnBranch is called for each branch being deleted
	OnBranch(name string, status Status, prNumber *int)

	// OnRestack is called when restacking children
	OnRestack(childCount int)

	// Complete is called when delete finishes
	Complete(deleted, skipped int)

	// Cleanup restores terminal state (may be no-op)
	Cleanup()

	// IsInteractive returns true if the handler supports interactive prompts
	IsInteractive() bool

	// PromptConfirm prompts user for confirmation before deleting
	PromptConfirm(branch string, reason string) (bool, error)
}

// NullHandler is a no-op handler for when nil is passed.
// It embeds handler.NullBase for Cleanup() and IsInteractive().
type NullHandler struct {
	handler.NullBase
}

// Start implements Handler.
func (h *NullHandler) Start(int) {}

// OnBranch implements Handler.
func (h *NullHandler) OnBranch(string, Status, *int) {}

// OnRestack implements Handler.
func (h *NullHandler) OnRestack(int) {}

// Complete implements Handler.
func (h *NullHandler) Complete(int, int) {}

// PromptConfirm implements Handler.
func (h *NullHandler) PromptConfirm(string, string) (bool, error) { return false, nil }
