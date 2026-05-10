package foreach

import (
	"github.com/getstackit/stackit/internal/tui/components/tree"
)

// Event represents a feedback event from the foreach action.
// Implementations should use type switches to handle specific event types.
type Event interface {
	foreachEvent() // marker method for type safety
}

// StackDisplayEvent indicates the initial stack visualization phase.
// Handlers can use this to display the branches that will be processed.
type StackDisplayEvent struct {
	Stack   *tree.StackTree // tree structure for rendering the stack
	Command string          // command being executed
}

func (StackDisplayEvent) foreachEvent() {}

// ExecutionStartEvent indicates the execution phase is beginning.
type ExecutionStartEvent struct {
	Branches []BranchInfo
}

func (ExecutionStartEvent) foreachEvent() {}

// BranchProgressEvent indicates per-branch execution progress.
type BranchProgressEvent struct {
	BranchName string
	Status     BranchStatus
	Output     string // command output (may be truncated)
	Error      error  // set on failure
}

func (BranchProgressEvent) foreachEvent() {}

// CompletionEvent indicates the action has finished.
type CompletionEvent struct {
	Success bool
	Message string
	Results []BranchResult // Consolidated results for all branches
}

func (CompletionEvent) foreachEvent() {}

// BranchResult contains the final result for a branch
type BranchResult struct {
	BranchName string
	Status     BranchStatus
	ExitCode   int
	Output     string
	Error      error
}

// BranchStatus represents the status of a branch during execution.
type BranchStatus string

// BranchStatus values for tracking execution progress.
const (
	StatusPending BranchStatus = "pending"
	StatusRunning BranchStatus = "running"
	StatusDone    BranchStatus = "done"
	StatusError   BranchStatus = "error"
	StatusSkipped BranchStatus = "skipped"
)

// BranchInfo contains information about a branch for execution tracking.
type BranchInfo struct {
	Name string
}
