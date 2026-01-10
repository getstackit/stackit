package fold

// Step represents a step in the fold process
type Step string

// Fold step constants
const (
	StepValidating Step = "validating"
	StepFolding    Step = "folding"
	StepRestacking Step = "restacking"
	StepCleanup    Step = "cleanup"
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
	OnStep(step Step, status StepStatus, message string)

	// Complete is called when fold finishes
	Complete(result Result)

	// Cleanup restores terminal state (may be no-op)
	Cleanup()
}

// NullHandler is a no-op handler for when nil is passed
type NullHandler struct{}

// Start implements Handler.
func (h *NullHandler) Start(_ string, _ string, _ bool) {}

// OnStep implements Handler.
func (h *NullHandler) OnStep(_ Step, _ StepStatus, _ string) {}

// Complete implements Handler.
func (h *NullHandler) Complete(_ Result) {}

// Cleanup implements Handler.
func (h *NullHandler) Cleanup() {}
