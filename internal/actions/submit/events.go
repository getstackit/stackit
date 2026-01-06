package submit

import (
	"stackit.dev/stackit/internal/tui/components/tree"
)

// Event represents a feedback event from the submit action.
// Implementations should use type switches to handle specific event types.
type Event interface {
	submitEvent() // marker method for type safety
}

// StackDisplayEvent indicates the initial stack visualization phase.
// Handlers can use this to display the branches that will be processed.
type StackDisplayEvent struct {
	Stack    *tree.StackTree   // tree structure for rendering the stack
	FixedMap map[string]bool   // branch -> is fixed (doesn't need restack)
	ScopeMap map[string]string // branch -> scope
}

func (StackDisplayEvent) submitEvent() {}

// RestackEvent indicates restack phase status.
type RestackEvent struct {
	Started   bool
	Completed bool
}

func (RestackEvent) submitEvent() {}

// PreparingEvent indicates the preparation/validation phase has started.
type PreparingEvent struct{}

func (PreparingEvent) submitEvent() {}

// BranchPlanEvent indicates what will happen to each branch.
type BranchPlanEvent struct {
	BranchName string
	Action     string // "create" or "update"
	IsCurrent  bool
	Skipped    bool
	SkipReason string
}

func (BranchPlanEvent) submitEvent() {}

// SubmissionStartEvent indicates the submission phase is beginning.
type SubmissionStartEvent struct {
	Branches []BranchInfo
}

func (SubmissionStartEvent) submitEvent() {}

// BranchProgressEvent indicates per-branch submission progress.
type BranchProgressEvent struct {
	BranchName string
	Status     BranchStatus
	URL        string // set on success
	Error      error  // set on failure
}

func (BranchProgressEvent) submitEvent() {}

// CompletionEvent indicates the action has finished.
type CompletionEvent struct {
	Success bool
	Message string // "All PRs up to date", "Dry run complete", etc.
}

func (CompletionEvent) submitEvent() {}

// BranchStatus represents the status of a branch during submission.
type BranchStatus string

// BranchStatus values for tracking submission progress.
const (
	StatusPending    BranchStatus = "pending"
	StatusSubmitting BranchStatus = "submitting"
	StatusSyncing    BranchStatus = "syncing"
	StatusDone       BranchStatus = "done"
	StatusError      BranchStatus = "error"
	StatusSkipped    BranchStatus = "skipped"
)

// BranchInfo contains information about a branch for submission tracking.
type BranchInfo struct {
	Name     string
	Action   string // "create" or "update"
	PRNumber *int
}
