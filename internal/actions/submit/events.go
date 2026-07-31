package submit

import (
	"time"

	"github.com/getstackit/stackit/internal/engine"
)

// Event represents a feedback event from the submit action.
// Implementations should use type switches to handle specific event types.
type Event interface {
	submitEvent() // marker method for type safety
}

// StackSnapshot contains the branch relationships and metadata needed to render
// the stack being submitted. This is action-layer data; adapters decide how to
// visualize it.
type StackSnapshot struct {
	Branches      []string          // branches in the stack, in order
	CurrentBranch string            // currently checked out branch
	TrunkBranch   string            // trunk/main branch name
	ParentMap     map[string]string // branch -> parent
	FixedMap      map[string]bool   // branch -> is fixed (doesn't need restack)
	ScopeMap      map[string]string // branch -> scope
	WorktreeMap   map[string]string // branch -> worktree path (for stack roots with managed worktrees)
}

// StackDisplayEvent indicates the initial stack visualization phase.
// Handlers can use this to display the branches that will be processed.
type StackDisplayEvent struct {
	Stack StackSnapshot
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
	Action     engine.SubmitAction
	PRNumber   *int // existing PR number for updates, nil for creates
	IsCurrent  bool
	Empty      bool // branch has no commits relative to its parent
	Skipped    bool
	SkipReason string
}

func (BranchPlanEvent) submitEvent() {}

// PlanningCompleteEvent indicates that all BranchPlanEvents have been emitted.
// Handlers that buffer plan rows can render the complete plan before any
// confirmation prompt or submission starts.
type PlanningCompleteEvent struct{}

func (PlanningCompleteEvent) submitEvent() {}

// BranchWarningEvent surfaces a non-fatal warning raised while submitting a
// branch (e.g. labels or reviewers could not be applied). Warnings must flow
// through the handler rather than direct console output: the interactive
// runner quiets the console while the TUI is active, so direct writes during
// the submission phase are silently dropped.
type BranchWarningEvent struct {
	BranchName string
	Warning    string
}

func (BranchWarningEvent) submitEvent() {}

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

// GitHubStackCreatedEvent reports that submit created GitHub's native Stack
// metadata after every PR in the selected chain was submitted.
type GitHubStackCreatedEvent struct {
	Number       int
	PullRequests []int
}

func (GitHubStackCreatedEvent) submitEvent() {}

// CompletionOutcome classifies how a submit run ended, so handlers can decide
// presentation without string-matching messages.
type CompletionOutcome string

// CompletionOutcome values.
const (
	OutcomeComplete        CompletionOutcome = "complete"          // branches were submitted
	OutcomeUpToDate        CompletionOutcome = "up-to-date"        // nothing needed submitting
	OutcomeDryRun          CompletionOutcome = "dry-run"           // plan reported, nothing submitted
	OutcomeCanceled        CompletionOutcome = "canceled"          // user declined a confirmation
	OutcomeNothingToSubmit CompletionOutcome = "nothing-to-submit" // no branches in scope / untracked
	OutcomeOnTrunk         CompletionOutcome = "on-trunk"          // checked out on trunk; nothing to submit from here
	OutcomeFailed          CompletionOutcome = "failed"            // at least one branch failed
)

// CompletionEvent indicates the action has finished.
type CompletionEvent struct {
	Outcome  CompletionOutcome
	Message  string        // human-readable detail, e.g. "All PRs up to date"
	Duration time.Duration // elapsed run time; set when branches were submitted
}

// Success reports whether the run ended without failure or cancellation.
func (e CompletionEvent) Success() bool {
	return e.Outcome != OutcomeFailed && e.Outcome != OutcomeCanceled
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
	Action   engine.SubmitAction
	PRNumber *int
}
