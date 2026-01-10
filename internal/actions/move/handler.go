package move

// Step represents a step in the move process
type Step string

// Move step constants
const (
	StepValidating  Step = "validating"
	StepReparenting Step = "reparenting"
	StepRestacking  Step = "restacking"
)

// StepStatus represents the status of a step
type StepStatus string

// Step status constants
const (
	StatusStarted   StepStatus = "started"
	StatusCompleted StepStatus = "completed"
	StatusSkipped   StepStatus = "skipped"
	StatusFailed    StepStatus = "failed"
)

// Result contains the result of the move action
type Result struct {
	SourceBranch string
	OldParent    string
	NewParent    string
	Renamed      bool
	NewName      string
}

// Handler receives events from move action
type Handler interface {
	// Start is called at the beginning of move
	Start(sourceBranch, oldParent, newParent string)

	// OnStep is called for each step in the move process
	OnStep(step Step, status StepStatus, message string)

	// OnRename is called when a branch is renamed due to scope change
	OnRename(oldName, newName string)

	// Complete is called when move finishes
	Complete(result Result)

	// Cleanup restores terminal state (may be no-op)
	Cleanup()

	// IsInteractive returns true if the handler supports interactive prompts
	IsInteractive() bool

	// PromptRename prompts user to confirm branch rename due to scope change
	PromptRename(oldName, oldScope, newScope string) (bool, error)
}

// NullHandler is a no-op handler for when nil is passed
type NullHandler struct{}

// Start implements Handler.
func (h *NullHandler) Start(_ string, _ string, _ string) {}

// OnStep implements Handler.
func (h *NullHandler) OnStep(_ Step, _ StepStatus, _ string) {}

// OnRename implements Handler.
func (h *NullHandler) OnRename(_ string, _ string) {}

// Complete implements Handler.
func (h *NullHandler) Complete(_ Result) {}

// Cleanup implements Handler.
func (h *NullHandler) Cleanup() {}

// IsInteractive implements Handler.
func (h *NullHandler) IsInteractive() bool { return false }

// PromptRename implements Handler.
func (h *NullHandler) PromptRename(_ string, _ string, _ string) (bool, error) { return false, nil }
