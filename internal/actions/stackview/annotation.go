package stackview

import (
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/internal/github"
)

// CheckState is the aggregate CI state of a branch's PR. The zero value means
// no CI status is known.
type CheckState string

// CheckState values.
const (
	CheckUnknown CheckState = ""
	CheckPassing CheckState = "passing"
	CheckFailing CheckState = "failing"
	CheckPending CheckState = "pending"
)

// ReviewState is the review decision on a branch's PR. The zero value means
// no review decision is known.
type ReviewState string

// ReviewState values.
const (
	ReviewUnknown          ReviewState = ""
	ReviewApproved         ReviewState = "approved"
	ReviewChangesRequested ReviewState = "changes_requested"
	ReviewAwaiting         ReviewState = "review_required"
)

// BranchAnnotation is the display data for one branch in a stack view:
// branch state, PR metadata, git-derived stats, forge (CI/review) status, and
// worktree ownership. Adapters convert it into their own render types.
type BranchAnnotation struct {
	IsLocked      bool
	IsFrozen      bool
	Scope         string
	ExplicitScope string

	// LocalSHA is the short commit SHA of the branch head.
	LocalSHA string

	PRNumber *git.PRNumber
	PRState  git.PRState
	PRURL    string
	IsDraft  bool

	CommitCount    int
	CommitMessages []string
	LinesAdded     int
	LinesDeleted   int

	// MergedDownstack holds historical merged parents.
	MergedDownstack []git.MergedParent

	Check  CheckState
	Review ReviewState

	// WorktreePath is set when the branch's stack is owned by a managed worktree.
	WorktreePath engine.WorktreePath
	// IsEmptyWorktree marks a worktree anchor with no child branches.
	IsEmptyWorktree bool
}

// AnnotationOptions controls which expensive git-derived fields are skipped.
// The zero value populates every field — opt out of work, never into it, so
// callers that forget the struct still get a complete annotation.
type AnnotationOptions struct {
	SkipCommitMessages bool
	SkipDiffStats      bool
}

// ScopeReader resolves a branch's effective scope.
type ScopeReader interface {
	GetScope(branch engine.Branch) engine.Scope
}

// AnnotationReader is the engine surface annotations with worktree info read.
type AnnotationReader interface {
	ScopeReader
	stackRootResolver
}

// Enrichment holds data resolved once for a whole view and joined into each
// branch's annotation: forge status (fetched separately from branch state) and
// the managed worktree index.
type Enrichment struct {
	CIStatuses github.ChecksByBranch
	Worktrees  *WorktreeIndex
}

// MinimalAnnotation returns the instant fields of an annotation — branch
// state, scope, and worktree info — with no git or network work, for fast
// initial rendering before full data is loaded.
func MinimalAnnotation(eng AnnotationReader, branch engine.Branch, worktrees *WorktreeIndex) BranchAnnotation {
	ann := BranchAnnotation{
		IsLocked:      branch.IsLocked(),
		IsFrozen:      branch.IsFrozen(),
		Scope:         eng.GetScope(branch).String(),
		ExplicitScope: branch.GetExplicitScope().String(),
	}
	worktrees.apply(eng, branch, &ann)
	return ann
}

// FullAnnotation returns BaseAnnotation joined with the enrichment's CI and
// review status and worktree info. A nil enrichment yields the base annotation.
func FullAnnotation(eng AnnotationReader, branch engine.Branch, stat engine.BranchStat, enrichment *Enrichment, opts AnnotationOptions) BranchAnnotation {
	ann := BaseAnnotation(eng, branch, stat, opts)
	if enrichment == nil {
		return ann
	}

	if !branch.IsTrunk() {
		if status := enrichment.CIStatuses.Get(branch.GetName()); status != nil {
			ann.Check = checkState(status)
			ann.Review = reviewState(status.ReviewDecision)
		}
	}

	enrichment.Worktrees.apply(eng, branch, &ann)
	return ann
}

// BaseAnnotation returns a branch's annotation without forge or worktree
// enrichment. The git-computed fields (short SHA, commit count, diff stats)
// come from stat, a batched value the caller resolves with BatchBranchStats —
// code annotating a set of branches must build that batch rather than reading
// per branch.
func BaseAnnotation(eng ScopeReader, branch engine.Branch, stat engine.BranchStat, opts AnnotationOptions) BranchAnnotation {
	ann := BranchAnnotation{
		IsLocked:      branch.IsLocked(),
		IsFrozen:      branch.IsFrozen(),
		Scope:         eng.GetScope(branch).String(),
		ExplicitScope: branch.GetExplicitScope().String(),
		LocalSHA:      stat.ShortSHA,
	}

	if branch.IsTrunk() {
		return ann
	}

	if prInfo, _ := branch.GetPrInfo(); prInfo != nil {
		ann.PRNumber = prInfo.Number()
		ann.PRState = prInfo.State()
		ann.IsDraft = prInfo.IsDraft()
		ann.PRURL = prInfo.URL()
	}

	// Commit messages for the detailed view; otherwise the batched count.
	if !opts.SkipCommitMessages {
		if commits, err := branch.GetAllCommits(); err == nil {
			ann.CommitMessages = commits.Onelines()
			ann.CommitCount = len(commits)
		}
	} else {
		ann.CommitCount = stat.CommitCount
	}

	if merged := branch.GetMergedDownstack(); len(merged) > 0 {
		ann.MergedDownstack = merged
	}

	if !opts.SkipDiffStats {
		ann.LinesAdded = stat.LinesAdded
		ann.LinesDeleted = stat.LinesDeleted
	}

	return ann
}

func checkState(status *github.CheckStatus) CheckState {
	switch {
	case status.Pending:
		return CheckPending
	case status.Passing:
		return CheckPassing
	default:
		return CheckFailing
	}
}

func reviewState(decision github.ReviewDecision) ReviewState {
	switch decision {
	case github.ReviewDecisionApproved:
		return ReviewApproved
	case github.ReviewDecisionChangesRequested:
		return ReviewChangesRequested
	case github.ReviewDecisionReviewRequired:
		return ReviewAwaiting
	default:
		return ReviewUnknown
	}
}
