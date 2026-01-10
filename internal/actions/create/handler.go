package create

// Step represents a step in the create process
type Step string

// Create step constants
const (
	StepStaging      Step = "staging"
	StepMessage      Step = "message"
	StepBranchCreate Step = "branch_create"
	StepCommit       Step = "commit"
	StepTracking     Step = "tracking"
	StepWorktree     Step = "worktree"
	StepScope        Step = "scope"
	StepInsert       Step = "insert"
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

// Result contains the result of the create action.
type Result struct {
	BranchName   string
	ParentBranch string
	HasCommit    bool
	WorktreePath string
}

// Handler receives events from create action
type Handler interface {
	// Start is called at the beginning of create
	Start(parentBranch string)

	// OnStep is called for each step in the create process
	OnStep(step Step, status StepStatus, message string)

	// Complete is called when create finishes
	Complete(result Result)

	// Cleanup restores terminal state (may be no-op)
	Cleanup()

	// IsInteractive returns true if the handler supports interactive prompts
	IsInteractive() bool

	// PromptStageChanges prompts user to stage unstaged changes
	PromptStageChanges() (bool, error)
}

// NullHandler is a no-op handler for when nil is passed
type NullHandler struct{}

// Start implements Handler.
func (h *NullHandler) Start(_ string) {}

// OnStep implements Handler.
func (h *NullHandler) OnStep(_ Step, _ StepStatus, _ string) {}

// Complete implements Handler.
func (h *NullHandler) Complete(_ Result) {}

// Cleanup implements Handler.
func (h *NullHandler) Cleanup() {}

// IsInteractive implements Handler.
func (h *NullHandler) IsInteractive() bool { return false }

// PromptStageChanges implements Handler.
func (h *NullHandler) PromptStageChanges() (bool, error) { return false, nil }
