package fold

import "github.com/getstackit/stackit/internal/actions/handler"

// Step represents a step in the fold process
type Step string

// Fold step constants
const (
	StepValidating Step = "validating"
	StepFolding    Step = "folding"
	StepRestacking Step = "restacking"
	StepCleanup    Step = "cleanup"
)

// Result contains the result of the fold action
type Result struct {
	FoldedBranch  string
	IntoBranch    string
	ChildrenCount int
}

// Handler receives events from fold action
type Handler interface {
	// Start is called at the beginning of fold
	Start(currentBranch, parentBranch string, dryRun bool)

	// OnStep is called for each step in the fold process
	OnStep(step Step, status handler.StepStatus, message string)

	// Complete is called when fold finishes
	Complete(result Result)

	// Cleanup restores terminal state (may be no-op)
	Cleanup()

	// IsInteractive returns true if the handler supports interactive prompts
	IsInteractive() bool
}

// NullHandler is a no-op handler for when nil is passed.
// It embeds handler.NullBase for Cleanup() and IsInteractive().
type NullHandler struct {
	handler.NullBase
	handler.NullProgress[Step]
}

// Start implements Handler.
func (h *NullHandler) Start(string, string, bool) {}

// Complete implements Handler.
func (h *NullHandler) Complete(Result) {}
