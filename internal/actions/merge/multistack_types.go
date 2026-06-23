package merge

// Exclusion reason constants for MultiStackExcluded
const (
	excludeReasonConflict  = "conflict"
	excludeReasonCIFailure = "ci_failure"
)

// MultiStackInfo represents a stack that can be merged in multi-stack mode
type MultiStackInfo struct {
	RootBranch  string   // Stack root branch name (direct child of trunk)
	AllBranches []string // All branches in the stack (root to tip, in order)
	PRCount     int      // Number of PRs in this stack
	Scope       string   // Stack scope if any
}

// MultiStackResult contains the result of a multi-stack merge operation
type MultiStackResult struct {
	IncludedStacks []MultiStackInfo     // Stacks that were successfully included
	ExcludedStacks []MultiStackExcluded // Stacks that were excluded with reasons
	PRNumber       int                  // Created PR number
	PRURL          string               // Created PR URL
	BranchName     string               // Consolidation branch name
}

// MultiStackExcluded represents a stack that was not included in the merge
type MultiStackExcluded struct {
	Stack  MultiStackInfo
	Reason string // excludeReasonConflict | excludeReasonCIFailure
}

// MultiStackOptions contains options specific to multi-stack merge
type MultiStackOptions struct {
	SelectedStacks []string // Stack roots selected by user (skips picker if provided)
	SkipLocalCI    bool     // Skip local CI validation
	Wait           bool     // Wait for CI and auto-merge
}
