// Package shippable provides analysis and management of shippable work.
// It determines which stacks are ready to be merged to trunk based on
// PR approval status, CI checks, and combinability with other stacks.
package shippable

import (
	"stackit.dev/stackit/internal/actions/merge"
)

// Status represents the overall shippability state of a stack.
type Status string

const (
	// StatusShippable indicates the stack is ready to ship (all checks pass, approved).
	StatusShippable Status = "shippable"
	// StatusPending indicates the stack is waiting on CI or review.
	StatusPending Status = "pending"
	// StatusBlocked indicates the stack cannot ship (CI failed or changes requested).
	StatusBlocked Status = "blocked"
	// StatusIncomplete indicates the stack is missing PRs or has drafts.
	StatusIncomplete Status = "incomplete"
)

// BlockingReason describes why a PR is blocking shippability.
type BlockingReason string

const (
	// ReasonChangesRequested indicates a reviewer has requested changes.
	ReasonChangesRequested BlockingReason = "changes_requested"
	// ReasonCIFailing indicates CI checks are failing.
	ReasonCIFailing BlockingReason = "ci_failing"
	// ReasonCIPending indicates CI checks are still running.
	ReasonCIPending BlockingReason = "ci_pending"
	// ReasonDraft indicates the PR is still a draft.
	ReasonDraft BlockingReason = "draft"
	// ReasonNoPR indicates no PR exists for this branch.
	ReasonNoPR BlockingReason = "no_pr"
	// ReasonReviewRequired indicates the PR needs review approval.
	ReasonReviewRequired BlockingReason = "review_required"
	// ReasonNotPushed indicates the local branch differs from remote.
	ReasonNotPushed BlockingReason = "not_pushed"
)

// BlockingPR describes a PR that is blocking a stack from being shippable.
type BlockingPR struct {
	Branch   string         // Branch name
	PRNumber int            // PR number (0 if no PR)
	Reason   BlockingReason // Why this PR is blocking
}

// Stack represents a stack with its shippability analysis.
type Stack struct {
	Stack  merge.MultiStackInfo // The underlying stack
	Status Status               // Overall shippability status
	Author string               // GitHub username of stack author (from first PR)

	// Breakdown of shippability components
	ApprovalOK bool  // All PRs have been approved
	GitHubCIOK bool  // All GitHub CI checks are passing
	LocalCIOK  *bool // Local CI result (nil = not checked)

	// Blocking details
	BlockingPRs []BlockingPR // PRs blocking shippability

	// Compatibility (populated during combination analysis)
	CompatibleWith []string // Root branches this can ship with
	ConflictsWith  []string // Root branches this conflicts with
}

// IsShippable returns true if the stack is ready to ship.
func (s *Stack) IsShippable() bool {
	return s.Status == StatusShippable
}

// IsPending returns true if the stack is waiting on CI or review.
func (s *Stack) IsPending() bool {
	return s.Status == StatusPending
}

// IsBlocked returns true if the stack has failed CI or has changes requested.
func (s *Stack) IsBlocked() bool {
	return s.Status == StatusBlocked
}

// IsIncomplete returns true if the stack is missing PRs or has drafts.
func (s *Stack) IsIncomplete() bool {
	return s.Status == StatusIncomplete
}

// BranchCount returns the total number of branches in this stack.
func (s *Stack) BranchCount() int {
	return len(s.Stack.AllBranches)
}

// RootBranch returns the root branch name of this stack.
func (s *Stack) RootBranch() string {
	return s.Stack.RootBranch
}

// AnalysisResult contains the result of analyzing all stacks.
type AnalysisResult struct {
	Stacks          []Stack // All analyzed stacks
	ShippableCount  int     // Number of shippable stacks
	PendingCount    int     // Number of pending stacks
	BlockedCount    int     // Number of blocked stacks
	IncompleteCount int     // Number of incomplete stacks
}

// GetShippable returns only the stacks that are ready to ship.
func (r *AnalysisResult) GetShippable() []Stack {
	var result []Stack
	for _, s := range r.Stacks {
		if s.IsShippable() {
			result = append(result, s)
		}
	}
	return result
}

// GetPending returns only the stacks that are pending.
func (r *AnalysisResult) GetPending() []Stack {
	var result []Stack
	for _, s := range r.Stacks {
		if s.IsPending() {
			result = append(result, s)
		}
	}
	return result
}

// GetBlocked returns only the stacks that are blocked.
func (r *AnalysisResult) GetBlocked() []Stack {
	var result []Stack
	for _, s := range r.Stacks {
		if s.IsBlocked() {
			result = append(result, s)
		}
	}
	return result
}

// GetIncomplete returns only the stacks that are incomplete.
func (r *AnalysisResult) GetIncomplete() []Stack {
	var result []Stack
	for _, s := range r.Stacks {
		if s.IsIncomplete() {
			result = append(result, s)
		}
	}
	return result
}

// HasShippable returns true if there are any shippable stacks.
func (r *AnalysisResult) HasShippable() bool {
	return r.ShippableCount > 0
}

// TotalStacks returns the total number of stacks analyzed.
func (r *AnalysisResult) TotalStacks() int {
	return len(r.Stacks)
}

// FilterByAuthor returns a new AnalysisResult containing only stacks by the given author.
func (r *AnalysisResult) FilterByAuthor(author string) *AnalysisResult {
	if author == "" {
		return r
	}

	result := &AnalysisResult{
		Stacks: make([]Stack, 0),
	}

	for _, s := range r.Stacks {
		if s.Author == author {
			result.Stacks = append(result.Stacks, s)
			switch s.Status {
			case StatusShippable:
				result.ShippableCount++
			case StatusPending:
				result.PendingCount++
			case StatusBlocked:
				result.BlockedCount++
			case StatusIncomplete:
				result.IncompleteCount++
			}
		}
	}

	return result
}

// CombinationResult contains the result of checking if stacks can be merged together.
type CombinationResult struct {
	// Combinable indicates whether the selected stacks can be merged without conflicts.
	Combinable bool

	// WorkingStacks are stacks that can be merged together.
	WorkingStacks []Stack

	// ConflictingStacks are stacks that conflict and cannot be included.
	ConflictingStacks []ExcludedStack

	// LocalCIPassed indicates whether local CI passed on the combined code.
	// nil means local CI was not run.
	LocalCIPassed *bool

	// LocalCIError contains the error if local CI failed.
	LocalCIError error
}

// ExcludedStack represents a stack that was excluded from a combination.
type ExcludedStack struct {
	Stack  Stack           // The excluded stack
	Reason ExclusionReason // Why it was excluded
}

// ExclusionReason describes why a stack was excluded from a combination.
type ExclusionReason string

const (
	// ReasonMergeConflict indicates the stack has merge conflicts with others.
	ReasonMergeConflict ExclusionReason = "merge_conflict"
	// ReasonLocalCIFailed indicates local CI failed when this stack was included.
	ReasonLocalCIFailed ExclusionReason = "local_ci_failed"
)

// IncludedCount returns the number of stacks that can be combined.
func (r *CombinationResult) IncludedCount() int {
	return len(r.WorkingStacks)
}

// ExcludedCount returns the number of stacks that were excluded.
func (r *CombinationResult) ExcludedCount() int {
	return len(r.ConflictingStacks)
}

// AllCombined returns true if all stacks could be combined.
func (r *CombinationResult) AllCombined() bool {
	return len(r.ConflictingStacks) == 0
}

// GetWorkingRoots returns the root branch names of working stacks.
func (r *CombinationResult) GetWorkingRoots() []string {
	roots := make([]string, len(r.WorkingStacks))
	for i, s := range r.WorkingStacks {
		roots[i] = s.RootBranch()
	}
	return roots
}

// GetConflictingRoots returns the root branch names of conflicting stacks.
func (r *CombinationResult) GetConflictingRoots() []string {
	roots := make([]string, len(r.ConflictingStacks))
	for i, s := range r.ConflictingStacks {
		roots[i] = s.Stack.RootBranch()
	}
	return roots
}
