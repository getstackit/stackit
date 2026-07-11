package merge

import "fmt"

// ExcludeReason says why a stack was left out of a multi-stack merge.
type ExcludeReason string

const (
	excludeReasonConflict  ExcludeReason = "conflict"
	excludeReasonCIFailure ExcludeReason = "ci_failure"
)

// MultiStackInfo represents a stack that can be merged in multi-stack mode
type MultiStackInfo struct {
	RootBranch  string   // Stack root branch name (direct child of trunk)
	AllBranches []string // All branches in the stack (root to tip, in order)
	PRCount     int      // Number of PRs in this stack
	Scope       string   // Stack scope if any
}

// MultiStacks is an ordered collection of independent stacks.
type MultiStacks []MultiStackInfo

// Label returns the display label for a stack.
func (s MultiStackInfo) Label() string {
	label := fmt.Sprintf("%s (%d branches", s.RootBranch, len(s.AllBranches))
	if s.PRCount > 0 {
		label += fmt.Sprintf(", %d PRs", s.PRCount)
	}
	if s.Scope != "" {
		label += fmt.Sprintf(", scope: %s", s.Scope)
	}
	return label + ")"
}

// FilterByRoots returns stacks selected by root name, preserving selection order.
func (stacks MultiStacks) FilterByRoots(selectedRoots []string) MultiStacks {
	if len(selectedRoots) == 0 {
		return stacks
	}
	byRoot := make(map[string]MultiStackInfo, len(stacks))
	for _, stack := range stacks {
		byRoot[stack.RootBranch] = stack
	}
	filtered := make(MultiStacks, 0, len(selectedRoots))
	for _, root := range selectedRoots {
		if stack, ok := byRoot[root]; ok {
			filtered = append(filtered, stack)
		}
	}
	return filtered
}

// MultiStackResult contains the result of a multi-stack merge operation
type MultiStackResult struct {
	IncludedStacks MultiStacks          // Stacks that were successfully included
	ExcludedStacks []MultiStackExcluded // Stacks that were excluded with reasons
	PRNumber       int                  // Created PR number
	PRURL          string               // Created PR URL
	BranchName     string               // Consolidation branch name
}

// MultiStackExcluded represents a stack that was not included in the merge
type MultiStackExcluded struct {
	Stack  MultiStackInfo
	Reason ExcludeReason
}

// MultiStackOptions contains options specific to multi-stack merge
type MultiStackOptions struct {
	SelectedStacks []string // Stack roots selected by user (skips picker if provided)
	SkipLocalCI    bool     // Skip local CI validation
	Wait           bool     // Wait for CI and auto-merge
}
