package flatten

import basehandler "github.com/getstackit/stackit/internal/actions/handler"

// Step represents a step in the flatten process
type Step string

// Flatten step constants
const (
	StepAnalyzing  Step = "analyzing"
	StepValidating Step = "validating"
	StepFlattening Step = "flattening"
	StepRestacking Step = "restacking"
)

// ExcludedBranch represents a branch that was kept in place due to code dependencies
type ExcludedBranch struct {
	Branch string // Branch name
	Reason string // Why it was kept in place (e.g., "X depends on this branch")
}

// Preview contains information about the planned flatten for confirmation
type Preview struct {
	Moves            []basehandler.Reparent // Branches that will be moved
	UnchangedCount   int                    // Number of branches that won't change
	ExcludedBranches []ExcludedBranch       // Branches kept in place due to dependencies
}

// Result contains the result of the flatten action
type Result struct {
	MovedCount     int // Number of branches that were moved
	UnchangedCount int // Number of branches that stayed in place
}

// Handler receives events from flatten action
type Handler interface {
	// Start is called at the beginning of flatten
	Start(branchCount int)

	// OnStep is called for each step in the flatten process
	OnStep(step Step, status basehandler.StepStatus, message string)

	// OnValidationProgress is called during branch validation to report progress
	OnValidationProgress(current, total int, branchName string)

	// OnBranchMoved is called when a branch is moved to a new parent
	OnBranchMoved(move basehandler.Reparent)

	// Complete is called when flatten finishes
	Complete(result Result)

	// Cleanup restores terminal state (may be no-op)
	Cleanup()

	// IsInteractive returns true if the handler supports interactive prompts
	IsInteractive() bool

	// PromptConfirmFlatten displays a preview of the flatten and asks for confirmation.
	// Returns true to proceed with the flatten, false to cancel.
	// In non-interactive mode, returns true (auto-confirm).
	PromptConfirmFlatten(preview Preview) (bool, error)
}

// NullHandler is a no-op handler for when nil is passed.
// It embeds basehandler.NullBase for Cleanup() and IsInteractive(),
// and basehandler.NullProgress[Step] for OnStep().
type NullHandler struct {
	basehandler.NullBase
	basehandler.NullProgress[Step]
}

// Start implements Handler.
func (h *NullHandler) Start(int) {}

// OnValidationProgress implements Handler.
func (h *NullHandler) OnValidationProgress(int, int, string) {}

// OnBranchMoved implements Handler.
func (h *NullHandler) OnBranchMoved(basehandler.Reparent) {}

// Complete implements Handler.
func (h *NullHandler) Complete(Result) {}

// PromptConfirmFlatten implements Handler. Returns true (auto-confirm) for null handler.
func (h *NullHandler) PromptConfirmFlatten(Preview) (bool, error) { return true, nil }
