package split

import "github.com/getstackit/stackit/internal/actions/handler"

// Step represents a step in the split process
type Step string

// Split step constants
const (
	StepValidating        Step = "validating"
	StepSelecting         Step = "selecting"
	StepApplying          Step = "applying"
	StepRestacking        Step = "restacking"
	StepChoosingType      Step = "choosing_type"
	StepChoosingDirection Step = "choosing_direction"
	StepStagingHunks      Step = "staging_hunks"
	StepCommitMessage     Step = "commit_message"
	StepBranchName        Step = "branch_name"
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
	OnStep(step Step, status handler.StepStatus, message string)

	// OnBranchCreated is called when a new branch is created during split
	OnBranchCreated(branchName string)

	// Complete is called when split finishes
	Complete(result ActionResult)

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
func (h *NullHandler) Start(string, Style) {}

// OnBranchCreated implements Handler.
func (h *NullHandler) OnBranchCreated(string) {}

// Complete implements Handler.
func (h *NullHandler) Complete(ActionResult) {}
