package engine

import (
	"context"
	"iter"
	"time"

	"stackit.dev/stackit/internal/git"
)

// StackNavigator handles stack relationship queries
type StackNavigator interface {
	AllBranches() []Branch
	CurrentBranch() *Branch
	Trunk() Branch
	GetBranch(branchName string) Branch
	GetChildren(branch Branch) []Branch
	GetChildrenWithStrategy(branch Branch, strategy SortStrategy) []Branch
	GetRelativeStack(branch Branch, rng StackRange) []Branch
	GetRelativeStackUpstack(branch Branch) []Branch
	GetRelativeStackDownstack(branch Branch) []Branch
	GetFullStack(branch Branch) []Branch
	BranchesDepthFirst(startBranch Branch) iter.Seq2[Branch, int]
	SortBranchesTopologically(branches []Branch) []Branch
	FindBranchForCommit(commitSHA string) (string, error)
	ValidateOnBranch() (string, error)
	IsBranchEmpty(ctx context.Context, branchName string) (bool, error)
	GetScope(branch Branch) Scope
	GetRemote() string
	GetRepoInfo(ctx context.Context) (string, string, error)
	IsInsideRepo() bool
}

// BranchStatus provides branch state information
type BranchStatus interface {
	GetBranch(branchName string) Branch
	IsTrunk(branch Branch) bool
	IsTracked(branch Branch) bool
	IsUpToDate(branch Branch) bool
	IsMergedIntoTrunk(ctx context.Context, branchName string) (bool, error)
	IsBranchEmpty(ctx context.Context, branchName string) (bool, error)
	GetDeletionStatus(ctx context.Context, branchName string) (DeletionStatus, error)
	GetScope(branch Branch) Scope
	IsLocked(branch Branch) bool
	GetLockReason(branch Branch) LockReason
	IsFrozen(branch Branch) bool
	GetPrInfo(branch Branch) (*PrInfo, error)
	FindMostRecentTrackedAncestors(ctx context.Context, branchName string) ([]string, error)
	GetRemote() string
	GetRemoteURL(ctx context.Context) (string, error)
	GetBranchRemoteDifference(branchName string) (string, error)
	GetBranchRemoteStatus(branchName string) (BranchRemoteStatus, error)
	BranchMatchesRemote(branchName string) (bool, error)
	GetMergedBranches(ctx context.Context, target string) (map[string]bool, error)
}

// BranchInfo provides commit and diff metadata
type BranchInfo interface {
	GetCommitDate(branch Branch) (time.Time, error)
	GetCommitAuthor(branch Branch) (string, error)
	GetRevision(branch Branch) (string, error)
	GetCommitCount(branch Branch) (int, error)
	GetDiffStats(branch Branch) (added int, deleted int, err error)
	GetAllCommits(branch Branch, format CommitFormat) ([]string, error)
	GetParentCommitSHA(commitSHA string) (string, error)
	GetCommitSHA(branchName string, offset int) (string, error)
	GetRevisionForName(branchName string) (string, error)
	BatchGetRevisions(branchNames []string) (map[string]string, []error)
	GetCurrentRevision(ctx context.Context) (string, error)
	GetReflog(ctx context.Context, count int, format string) (string, error)
}

// GitDiffer handles diff and merge operations
type GitDiffer interface {
	GetMergeBase(rev1, rev2 string) (string, error)
	GetChangedFiles(ctx context.Context, base, head string) ([]string, error)
	IsDiffEmpty(ctx context.Context, base, head string) (bool, error)
	ShowDiff(ctx context.Context, left, right string, stat bool) (string, error)
	ShowCommits(ctx context.Context, base, head string, patch, stat bool) (string, error)
	IsAncestor(ancestor, descendant string) (bool, error)
}

// WorkingTree handles worktree and staging area operations
type WorkingTree interface {
	HasStagedChanges(ctx context.Context) (bool, error)
	HasUnstagedChanges(ctx context.Context) (bool, error)
	GetUnstagedDiff(ctx context.Context, files ...string) (string, error)
	GetPendingChanges(ctx context.Context) ([]PendingChange, error)
	GetCommitTemplate(ctx context.Context) (string, error)
	GetUnmergedFiles(ctx context.Context) ([]string, error)
	ParseStagedHunks(ctx context.Context) ([]git.Hunk, error)
	ListWorktrees(ctx context.Context) ([]string, error)
	IsRebaseInProgress(ctx context.Context) bool
	GetRebaseHead() (string, error)
	HasUncommittedChanges(ctx context.Context) bool
	CheckoutPaths(ctx context.Context, branch string, pathspecs []string) error
	RemovePaths(ctx context.Context, pathspecs []string) error
	StashList(ctx context.Context) (string, error)
}

// BranchReader is a composite interface for backward compatibility
// Prefer using the smaller, focused interfaces above for new code
type BranchReader interface {
	StackNavigator
	BranchStatus
	BranchInfo
	GitDiffer
	WorkingTree
}

// BranchTracking handles branch tracking operations
type BranchTracking interface {
	TrackBranch(ctx context.Context, branchName string, parentBranchName string) error
	UntrackBranch(branchName string) error
	SetParent(ctx context.Context, branch Branch, parentBranch Branch) error
	UpdateParentRevision(branchName string, parentRev string) error
	SetScope(branch Branch, scope Scope) error
	SetLocked(branches []Branch, reason LockReason) (BatchLockResult, error)
	SetFrozen(branches []Branch, frozen bool) (BatchFreezeResult, error)
}

// BranchMutations handles branch lifecycle operations
type BranchMutations interface {
	RenameBranch(ctx context.Context, oldBranch, newBranch Branch) error
	DeleteBranch(ctx context.Context, branch Branch) error
	DeleteBranches(ctx context.Context, branches []Branch) ([]string, error)
	CheckoutBranch(ctx context.Context, branch Branch) error
	CreateAndCheckoutBranch(ctx context.Context, branch Branch) error
	UpdateBranchRef(ctx context.Context, branchName, revision string) error
	CreateBranch(ctx context.Context, branchName string, startPoint string) error
	ResetHard(ctx context.Context, revision string) error
	ResetMerge(ctx context.Context, revision string) error
	Merge(ctx context.Context, revision string, opts MergeOptions) error
	Fetch(ctx context.Context, remote string, branch string) error
	InteractiveRebase(ctx context.Context, onto string) error
}

// CommitOperations handles staging and committing
type CommitOperations interface {
	Commit(ctx context.Context, message string, verbose int, noVerify bool) error
	CommitWithOptions(ctx context.Context, opts git.CommitOptions) error
	StageAll(ctx context.Context) error
	StagePatch(ctx context.Context) error
	StashPush(ctx context.Context, message string) (string, error)
	StashPop(ctx context.Context) error
}

// WorktreeOperations handles worktree management
type WorktreeOperations interface {
	AddWorktree(ctx context.Context, path string, branch string, detach bool) error
	RemoveWorktree(ctx context.Context, path string) error
	CreateTemporaryWorktree(ctx context.Context, branch string, prefix string) (path string, cleanup func(), err error)
}

// Initializer handles repository initialization operations
type Initializer interface {
	Reset(newTrunkName string) error
	Rebuild(newTrunkName string) error
}

// BranchWriter is a composite interface for backward compatibility
// Prefer using the smaller, focused interfaces above for new code
type BranchWriter interface {
	BranchTracking
	BranchMutations
	CommitOperations
	WorktreeOperations
	Initializer
}

// Absorber applies staged hunks to appropriate commits
type Absorber interface {
	ApplyHunksToBranch(ctx context.Context, branch Branch, hunksByCommit map[string][]git.Hunk) error
	FindTargetCommitForHunk(hunk git.Hunk, commitSHAs []string) (string, int, error)
}
