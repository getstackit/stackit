// Package combine provides functionality for combining multiple stacks into a single PR.
package combine

// StackInfo represents a stack that can be combined
type StackInfo struct {
	RootBranch  string   // Stack root branch name (direct child of trunk)
	AllBranches []string // All branches in the stack (root to tip, in order)
	PRCount     int      // Number of PRs in this stack
	Scope       string   // Stack scope if any
}

// Result contains the result of a combine operation
type Result struct {
	IncludedStacks []StackInfo     // Stacks that were successfully included
	ExcludedStacks []ExcludedStack // Stacks that were excluded with reasons
	PRNumber       int             // Created PR number
	PRURL          string          // Created PR URL
	BranchName     string          // Consolidation branch name
}

// ExcludedStack represents a stack that was not included in the combine
type ExcludedStack struct {
	Stack  StackInfo
	Reason string // "conflict" | "ci_failure"
}

// Options for the combine action
type Options struct {
	SelectedStacks []string // Stack roots selected by user (skips picker if provided)
	DryRun         bool     // Show what would be combined without executing
	Force          bool     // Skip validation checks
	Wait           bool     // Wait for CI and auto-merge
	SkipCI         bool     // Skip local CI validation
	Yes            bool     // Skip confirmation prompts
}
