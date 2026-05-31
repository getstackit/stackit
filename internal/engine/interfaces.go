package engine

import (
	"context"
	"iter"
	"time"

	"github.com/getstackit/stackit/internal/git"
)

// StackNavigator handles stack relationship queries
type StackNavigator interface {
	AllBranches() Branches
	BranchNames() *BranchSet
	CurrentBranch() *Branch
	Trunk() Branch
	GetBranch(branchName string) Branch
	Graph(strategy SortStrategy) *StackGraph
	BranchesDepthFirst(startBranch Branch) iter.Seq2[Branch, int]
	SortBranchesTopologically(branches Branches) Branches
	FindBranchForCommit(commitSHA string) (string, error)
	// FindNearestNonExcludedAncestor walks the parent chain from startParent
	// and returns the first ancestor for which isExcluded returns false. Falls
	// back to trunk if every ancestor up the chain is excluded.
	FindNearestNonExcludedAncestor(startParent string, isExcluded func(name string) bool) string
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
	ReadBranchStatuses(branches Branches) BranchStatuses
	IsMergedIntoTrunk(ctx context.Context, branchName string) (bool, error)
	IsBranchEmpty(ctx context.Context, branchName string) (bool, error)
	GetDeletionStatuses(ctx context.Context, branchNames []string) (map[string]DeletionStatus, error)
	GetScope(branch Branch) Scope
	GetStackDescription(branch Branch) *git.StackDescription
	IsLocked(branch Branch) bool
	GetLockReason(branch Branch) LockReason
	IsFrozen(branch Branch) bool
	IsWorktreeAnchor(branch Branch) bool
	GetBranchType(branch Branch) git.BranchType
	GetPrInfo(branch Branch) (*PrInfo, error)
	FindMostRecentTrackedAncestors(ctx context.Context, branchName string) ([]string, error)
	GetRemote() string
	GetRemoteURL(ctx context.Context) (string, error)
	GetBranchRemoteDifference(branchName string) (string, error)
	GetBranchRemoteStatus(branch Branch) (BranchRemoteStatus, error)
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
	GetRevisions(branchNames []string) (map[string]string, []error)
	GetCurrentRevision(ctx context.Context) (string, error)
	GetRecentTrunkCommits(count int) ([]git.RecentCommit, error)
	GetReflog(ctx context.Context, count int, format string) (string, error)
	// GetDivergencePoint returns the divergence point of a branch from its parent.
	// Returns the ParentBranchRevision from metadata if valid, otherwise the parent's current revision.
	GetDivergencePoint(branchName string) (string, error)
	// PreloadBranchData batch-loads metadata and revisions for all branches
	// into their respective caches. Call before parallel annotation building
	// to eliminate per-branch cache misses and mutex contention.
	PreloadBranchData()
	// PreloadBranchStats warms the diff-stats and commit-count caches for all
	// given branches in parallel. Call before utils.Run iteration so subsequent
	// GetDiffStats / GetCommitCount calls are instant cache hits.
	PreloadBranchStats(branches []Branch)
}

// GitDiffer handles diff and merge operations
type GitDiffer interface {
	GetMergeBase(ctx context.Context, rev1, rev2 string) (string, error)
	GetChangedFiles(ctx context.Context, base, head string) ([]string, error)
	IsDiffEmpty(ctx context.Context, base, head string) (bool, error)
	ShowDiff(ctx context.Context, left, right string, stat bool) (string, error)
	ShowCommits(ctx context.Context, base, head string, patch, stat bool) (string, error)
	IsAncestor(ctx context.Context, ancestor, descendant string) (bool, error)
	// GetDiffBetween returns raw diff between two refs, suitable for parsing into hunks.
	GetDiffBetween(ctx context.Context, base, head string, files ...string) (string, error)
}

// WorkingTree handles worktree and staging area operations
type WorkingTree interface {
	HasStagedChanges(ctx context.Context) (bool, error)
	HasUnstagedChanges(ctx context.Context) (bool, error)
	HasUntrackedFiles(ctx context.Context) (bool, error)
	// GetWorkingTreeStatus returns all three working-tree flags in one git call.
	// Prefer this over calling Has* individually when multiple flags are needed.
	GetWorkingTreeStatus(ctx context.Context) (staged, unstaged, untracked bool, err error)
	GetUnstagedDiff(ctx context.Context, files ...string) (string, error)
	GetUntrackedFileHunks(ctx context.Context) ([]git.Hunk, error)
	GetPendingChanges(ctx context.Context) ([]PendingChange, error)
	GetCommitTemplate(ctx context.Context) (string, error)
	GetUnmergedFiles(ctx context.Context) ([]string, error)
	ParseStagedHunks(ctx context.Context) ([]git.Hunk, error)
	ListWorktrees(ctx context.Context) (git.WorktreeList, error)
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
	// UntrackBranches stops tracking multiple branches, deleting their metadata
	// and triggering a single engine rebuild.
	UntrackBranches(ctx context.Context, branchNames []string) error
	SetParent(ctx context.Context, branch Branch, parentBranch Branch, mode DivergenceMode) error
	// ReparentBranch changes a branch's parent while automatically preserving
	// its divergence point. Preferred over SetParent for existing branches.
	ReparentBranch(ctx context.Context, branch Branch, newParent Branch) error
	// ReparentBranches changes multiple branches to the same new parent while
	// preserving divergence points. All divergence points are captured before
	// any reparenting begins.
	ReparentBranches(ctx context.Context, branchNames []string, newParent Branch) error
	SetScope(ctx context.Context, branch Branch, scope Scope) error
	// SetScopeAndMarkForUpdate sets the scope and marks the branch as needing a
	// PR body update in one atomic transaction instead of two separate ref writes.
	SetScopeAndMarkForUpdate(ctx context.Context, branch Branch, scope Scope) error
	SetBranchType(branch Branch, branchType git.BranchType) error
	SetLocked(ctx context.Context, branches Branches, reason LockReason) (BatchLockResult, error)
	SetFrozen(ctx context.Context, branches Branches, frozen bool) (BatchFreezeResult, error)

	// MarkBranchesForPRBodyUpdate marks multiple branches as needing a PR body
	// update in a single atomic operation.
	MarkBranchesForPRBodyUpdate(ctx context.Context, branchNames []string) error
	// ClearNeedsPRBodyUpdate clears the PR body update flag for a branch
	ClearNeedsPRBodyUpdate(branchName string) error
	// GetBranchesNeedingPRBodyUpdate returns all branches that need PR body updates
	GetBranchesNeedingPRBodyUpdate() []string

	// GetStackDescription returns the stack description for a branch's stack.
	// It first checks the stack ref, then falls back to legacy branch metadata.
	GetStackDescription(branch Branch) *git.StackDescription
	// SetStackDescription sets the stack description in the stack ref for a branch.
	// Returns an error if the branch is not part of a tracked stack.
	SetStackDescription(ctx context.Context, branch Branch, desc *git.StackDescription) error
	// ClearStackDescription removes the stack description from the stack ref.
	ClearStackDescription(ctx context.Context, branch Branch) error

	// GenerateStackID creates a new stack ID for a new stack.
	// Format: {timestamp-nanos}-{sanitized-root-branch}
	GenerateStackID(rootBranch string) string
	// GetStackID returns the stack ID for a branch.
	// Returns empty string for untracked branches or trunk.
	// For legacy branches without StackID, derives it from the stack root.
	GetStackID(branch Branch) string
	// EnsureStackID returns the stack ID for a branch, creating one if it doesn't exist.
	// This is used for lazy creation of stack metadata when setting descriptions or scopes.
	EnsureStackID(ctx context.Context, branch Branch) (string, error)
	// SetStackID sets the stack ID on multiple branches atomically in a single transaction.
	SetStackID(ctx context.Context, branches Branches, stackID string) error
	// AssignBranchesToNewStack creates a new stack metadata ref and assigns its
	// ID to the provided branches atomically.
	AssignBranchesToNewStack(ctx context.Context, root Branch, branches Branches) (string, error)
	// CreateStackRef creates a new stack ref with the given metadata.
	CreateStackRef(stackID string, meta *git.StackMeta) error
	// GetStackMeta returns the stack metadata for a stack ID.
	GetStackMeta(stackID string) (*git.StackMeta, error)
}

// BranchMutations handles branch lifecycle operations
type BranchMutations interface {
	RenameBranch(ctx context.Context, oldBranch, newBranch Branch) error
	DeleteBranch(ctx context.Context, branch Branch) error
	DeleteBranches(ctx context.Context, branches Branches) ([]string, error)
	CheckoutBranch(ctx context.Context, branch Branch) error
	CreateAndCheckoutBranch(ctx context.Context, branch Branch) error
	UpdateBranchRef(ctx context.Context, branchName, revision string) error
	CreateBranch(ctx context.Context, branchName string, startPoint string) error
	ResetHard(ctx context.Context, revision string) error
	ResetMerge(ctx context.Context, revision string) error
	Merge(ctx context.Context, revision string, opts MergeOptions) error
	MergeMultiple(ctx context.Context, branches []string, opts MergeOptions) error
	Fetch(ctx context.Context, remote string, branch string) error
	InteractiveRebase(ctx context.Context, onto string) error
}

// CommitOperations handles staging and committing
type CommitOperations interface {
	Commit(ctx context.Context, message string, verbose int, noVerify bool) error
	CommitWithOptions(ctx context.Context, opts git.CommitOptions) error
	StageAll(ctx context.Context) error
	StagePatch(ctx context.Context) error
	StageHunks(ctx context.Context, hunks []git.Hunk) error
	StashPush(ctx context.Context, message string) (string, error)
	StashPushStaged(ctx context.Context, message string) (string, error)
	StashPop(ctx context.Context) error
}

// WorktreeOperations handles worktree management
type WorktreeOperations interface {
	AddWorktree(ctx context.Context, path string, branch string, detach bool) error
	RemoveWorktree(ctx context.Context, path string) error
	ForceRemoveWorktree(ctx context.Context, path string) error
	CreateTemporaryWorktree(ctx context.Context, branch string, prefix string) (path string, cleanup func(), err error)
	// CreateTemporaryWorktreeSkipPrune is like CreateTemporaryWorktree but skips the automatic
	// PruneWorktrees() call. Use this when creating multiple worktrees in parallel after
	// manually calling PruneWorktrees() once, to avoid race conditions.
	CreateTemporaryWorktreeSkipPrune(ctx context.Context, branch string, prefix string) (path string, cleanup func(), err error)
	PruneWorktrees(ctx context.Context) error
}

// WorktreeInfo represents information about a stackit-managed worktree
type WorktreeInfo struct {
	Name         string    // User-provided name for display
	Path         string    // Absolute path to worktree
	AnchorBranch string    // Hidden worktree anchor branch name
	CreatedAt    time.Time // When worktree was created
	MainRepoDir  string    // Path to main repo
}

// WorktreeRegistry handles stackit-managed worktree tracking
type WorktreeRegistry interface {
	// RegisterWorktree registers a worktree for a stack root
	RegisterWorktree(stackRoot string, path string) error
	// RegisterWorktreeWithName registers a worktree with a user-friendly name
	RegisterWorktreeWithName(anchorBranch string, path string, name string) error
	// UnregisterWorktree removes worktree registration for a stack root
	UnregisterWorktree(ctx context.Context, stackRoot string) error
	// GetWorktreeForStack returns worktree info for a stack root, or nil if none
	GetWorktreeForStack(stackRoot string) (*WorktreeInfo, error)
	// ListManagedWorktrees returns all stackit-managed worktrees
	ListManagedWorktrees() ([]WorktreeInfo, error)
	// GetStackRootForBranch returns the stack root for a given branch
	GetStackRootForBranch(branch Branch) string
	// IsInManagedWorktree checks if the current directory is a stackit-managed worktree
	// Returns true and worktree info if in a managed worktree, false otherwise
	IsInManagedWorktree() (bool, *WorktreeInfo, error)
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
