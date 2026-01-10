package delete

// Status represents the status of a branch deletion
type Status string

// Delete status constants
const (
	StatusDeleted  Status = "deleted"
	StatusSkipped  Status = "skipped"
	StatusRestaked Status = "restacked"
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

// NullHandler is a no-op handler for when nil is passed
type NullHandler struct{}

// Start implements Handler.
func (h *NullHandler) Start(_ int) {}

// OnBranch implements Handler.
func (h *NullHandler) OnBranch(_ string, _ Status, _ *int) {}

// OnRestack implements Handler.
func (h *NullHandler) OnRestack(_ int) {}

// Complete implements Handler.
func (h *NullHandler) Complete(_, _ int) {}

// Cleanup implements Handler.
func (h *NullHandler) Cleanup() {}

// IsInteractive implements Handler.
func (h *NullHandler) IsInteractive() bool { return false }

// PromptConfirm implements Handler.
func (h *NullHandler) PromptConfirm(_ string, _ string) (bool, error) { return false, nil }
