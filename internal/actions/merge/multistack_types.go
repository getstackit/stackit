package merge

import (
	"github.com/getstackit/stackit/internal/actions/stackview"
	"github.com/getstackit/stackit/internal/git"
)

// ExcludeReason says why a stack was left out of a multi-stack merge.
type ExcludeReason string

const (
	excludeReasonConflict  ExcludeReason = "conflict"
	excludeReasonCIFailure ExcludeReason = "ci_failure"
)

// MultiStackInfo represents a stack that can be merged in multi-stack mode.
// Stack discovery and its types live in stackview; these aliases keep the
// merge package's names.
type MultiStackInfo = stackview.StackInfo

// MultiStacks is an ordered collection of independent stacks.
type MultiStacks = stackview.Stacks

// MultiStackResult contains the result of a multi-stack merge operation
type MultiStackResult struct {
	IncludedStacks MultiStacks          // Stacks that were successfully included
	ExcludedStacks []MultiStackExcluded // Stacks that were excluded with reasons
	PRNumber       git.PRNumber         // Created PR number
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
