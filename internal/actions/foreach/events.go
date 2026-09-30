package foreach

// Event represents a feedback event from the foreach action.
// Implementations should use type switches to handle specific event types.
type Event interface {
	foreachEvent() // marker method for type safety
}

// StackSnapshot contains the branch relationships needed to render the
// branches foreach will process. This is action-layer data; adapters decide
// how to visualize it.
type StackSnapshot struct {
	Branches      []string          // branches to process, in order (trunk excluded)
	CurrentBranch string            // currently checked out branch
	TrunkBranch   string            // trunk/main branch name
	ParentMap     map[string]string // branch -> parent (trunk for trunk-rooted branches)
}

// StackDisplayEvent indicates the initial stack visualization phase.
// Handlers can use this to display the branches that will be processed.
type StackDisplayEvent struct {
	Stack   StackSnapshot // branch relationships for rendering the stack
	Command string        // command being executed
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
