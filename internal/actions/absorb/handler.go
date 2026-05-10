package absorb

import (
	"github.com/getstackit/stackit/internal/actions/handler"
	"github.com/getstackit/stackit/internal/git"
)

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
	OnStep(step Step, status handler.StepStatus, message string)

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

// NullHandler is a no-op handler for when nil is passed.
// It embeds handler.NullBase for Cleanup() and IsInteractive().
type NullHandler struct {
	handler.NullBase
	handler.NullProgress[Step]
	handler.NullPromptHandler
}

// Start implements Handler.
func (h *NullHandler) Start(bool) {}

// OnHunkTarget implements Handler.
func (h *NullHandler) OnHunkTarget(string, string, string) {}

// OnUnabsorbedHunk implements Handler.
func (h *NullHandler) OnUnabsorbedHunk(git.Hunk) {}

// OnApply implements Handler.
func (h *NullHandler) OnApply(string, string) {}

// Complete implements Handler.
func (h *NullHandler) Complete(Result) {}
