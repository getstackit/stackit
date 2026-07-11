package pluck

import basehandler "github.com/getstackit/stackit/internal/actions/handler"

// Step represents a step in the pluck process
type Step string

// Pluck step constants
const (
	StepValidating        Step = "validating"
	StepReparentingChild  Step = "reparenting-child"
	StepMovingSource      Step = "moving-source"
	StepRestackingOrphans Step = "restacking-orphans"
)

// Result contains the result of the pluck action
type Result struct {
	SourceBranch       string   // Branch that was plucked
	OldParent          string   // Previous parent of source
	NewParent          string   // New parent of source
	ReparentedChildren []string // Children that were reparented to grandparent
}

// Preview contains information about the planned pluck for confirmation
type Preview struct {
	SourceBranch   string   // Branch being plucked
	OldParent      string   // Current parent of source
	NewParent      string   // Target parent for source
	Children       []string // Direct children that will be reparented
	ChildNewParent string   // Where children will be reparented (grandparent)
	Commits        []string // Commit subjects on the source branch
}

// Handler receives events from pluck action
type Handler interface {
	// Start is called at the beginning of pluck
	Start(move basehandler.Reparent)

	// OnStep is called for each step in the pluck process
	OnStep(step Step, status basehandler.StepStatus, message string)

	// OnChildReparented is called when a child is reparented to the grandparent
	OnChildReparented(move basehandler.Reparent)

	// Complete is called when pluck finishes
	Complete(result Result)

	// Cleanup restores terminal state (may be no-op)
	Cleanup()

	// IsInteractive returns true if the handler supports interactive prompts
	IsInteractive() bool

	// PromptConfirmPluck displays a preview of the pluck and asks for confirmation.
	// Returns true to proceed with the pluck, false to cancel.
	// In non-interactive mode, returns true (auto-confirm).
	PromptConfirmPluck(preview Preview) (bool, error)
}

// NullHandler is a no-op handler for when nil is passed.
// It embeds basehandler.NullBase for Cleanup() and IsInteractive(),
// and basehandler.NullProgress[Step] for OnStep().
type NullHandler struct {
	basehandler.NullBase
	basehandler.NullProgress[Step]
}

// Start implements Handler.
func (h *NullHandler) Start(basehandler.Reparent) {}

// OnChildReparented implements Handler.
func (h *NullHandler) OnChildReparented(basehandler.Reparent) {}

// Complete implements Handler.
func (h *NullHandler) Complete(Result) {}

// PromptConfirmPluck implements Handler. Returns true (auto-confirm) for null handler.
func (h *NullHandler) PromptConfirmPluck(Preview) (bool, error) { return true, nil }
