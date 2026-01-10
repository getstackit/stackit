package absorb

import "stackit.dev/stackit/internal/git"

// Step represents a step in the absorb process
type Step string

// Absorb step constants
const (
	StepStaging    Step = "staging"
	StepParsing    Step = "parsing"
	StepFinding    Step = "finding"
	StepConfirming Step = "confirming"
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

// HunkResult represents the result of absorbing a hunk
type HunkResult struct {
	File       string
	CommitSHA  string
	BranchName string
	LineStart  int
	LineEnd    int
}

// Result contains the result of the absorb action
type Result struct {
	Absorbed    int
	Unabsorbed  int
	BranchCount int
}

// Handler receives events from absorb action
type Handler interface {
	// Start is called at the beginning of absorb
	Start(dryRun bool)

	// OnStep is called for each step in the absorb process
	OnStep(step Step, status StepStatus, message string)

	// OnHunkTarget is called when a target is found for a hunk
	OnHunkTarget(file string, commitSHA string, branchName string)

	// OnUnabsorbedHunk is called for hunks that could not be absorbed
	OnUnabsorbedHunk(hunk git.Hunk)

	// OnApply is called when hunks are applied to a branch
	OnApply(branchName string, commitSHA string)

	// Complete is called when absorb finishes
	Complete(result Result)

	// Cleanup restores terminal state (may be no-op)
	Cleanup()

	// IsInteractive returns true if the handler supports interactive prompts
	IsInteractive() bool

	// PromptConfirm prompts user for confirmation before absorbing
	PromptConfirm(message string) (bool, error)
}

// NullHandler is a no-op handler for when nil is passed
type NullHandler struct{}

// Start implements Handler.
func (h *NullHandler) Start(_ bool) {}

// OnStep implements Handler.
func (h *NullHandler) OnStep(_ Step, _ StepStatus, _ string) {}

// OnHunkTarget implements Handler.
func (h *NullHandler) OnHunkTarget(_ string, _ string, _ string) {}

// OnUnabsorbedHunk implements Handler.
func (h *NullHandler) OnUnabsorbedHunk(_ git.Hunk) {}

// OnApply implements Handler.
func (h *NullHandler) OnApply(_ string, _ string) {}

// Complete implements Handler.
func (h *NullHandler) Complete(_ Result) {}

// Cleanup implements Handler.
func (h *NullHandler) Cleanup() {}

// IsInteractive implements Handler.
func (h *NullHandler) IsInteractive() bool { return false }

// PromptConfirm implements Handler.
func (h *NullHandler) PromptConfirm(_ string) (bool, error) { return false, nil }
