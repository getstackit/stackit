package split

// Step represents a step in the split process
type Step string

// Split step constants
const (
	StepValidating Step = "validating"
	StepSelecting  Step = "selecting"
	StepApplying   Step = "applying"
	StepRestacking Step = "restacking"
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

// ActionResult contains the result of the split action for the handler
type ActionResult struct {
	OriginalBranch string
	NewBranches    []string
	Style          Style
}

// Handler receives events from split action
type Handler interface {
	// Start is called at the beginning of split
	Start(branchName string, style Style)

	// OnStep is called for each step in the split process
	OnStep(step Step, status StepStatus, message string)

	// OnBranchCreated is called when a new branch is created during split
	OnBranchCreated(branchName string)

	// Complete is called when split finishes
	Complete(result ActionResult)

	// Cleanup restores terminal state (may be no-op)
	Cleanup()
}

// NullHandler is a no-op handler for when nil is passed
type NullHandler struct{}

// Start implements Handler.
func (h *NullHandler) Start(_ string, _ Style) {}

// OnStep implements Handler.
func (h *NullHandler) OnStep(_ Step, _ StepStatus, _ string) {}

// OnBranchCreated implements Handler.
func (h *NullHandler) OnBranchCreated(_ string) {}

// Complete implements Handler.
func (h *NullHandler) Complete(_ ActionResult) {}

// Cleanup implements Handler.
func (h *NullHandler) Cleanup() {}
