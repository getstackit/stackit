package create

import "github.com/getstackit/stackit/internal/actions/handler"

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
	OnStep(step Step, status handler.StepStatus, message string)

	// Complete is called when create finishes
	Complete(result Result)

	// Cleanup restores terminal state (may be no-op)
	Cleanup()

	// IsInteractive returns true if the handler supports interactive prompts
	IsInteractive() bool

	// PromptStageChanges prompts user to stage unstaged changes
	PromptStageChanges() (bool, error)

	// PromptScope prompts user for a scope value when pattern contains {scope}
	// The patternHint shows the current branch pattern to the user
	PromptScope(patternHint string) (string, error)

	// PromptChildToMove asks which of the current branch's children should be
	// moved onto a newly inserted branch. Returns "all" to move every child, or
	// a specific child name. Only called when IsInteractive() is true.
	PromptChildToMove(children []string) (string, error)
}

// NullHandler is a no-op handler for when nil is passed
type NullHandler struct {
	handler.NullBase
	handler.NullProgress[Step]
}

// Start implements Handler.
func (h *NullHandler) Start(_ string) {}

// Complete implements Handler.
func (h *NullHandler) Complete(_ Result) {}

// PromptStageChanges implements Handler.
func (h *NullHandler) PromptStageChanges() (bool, error) { return false, nil }

// PromptScope implements Handler.
func (h *NullHandler) PromptScope(_ string) (string, error) { return "", nil }

// PromptChildToMove implements Handler. Returns "all" (move every child).
func (h *NullHandler) PromptChildToMove(_ []string) (string, error) { return "all", nil }
