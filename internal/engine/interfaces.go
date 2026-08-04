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
	// CurrentBranchName returns the current branch name, or "" when HEAD is not
	// on a branch (e.g. a detached-HEAD server mirror). Nil-safe alternative to
	// CurrentBranch().GetName().
	CurrentBranchName() string
	Trunk() Branch
	GetBranch(branchName string) Branch
	Graph(strategy SortStrategy) *StackGraph
	BranchesDepthFirst(startBranch Branch) iter.Seq2[Branch, int]
	SortBranchesTopologically(branches Branches) Branches
	FindBranchesForCommits(commitSHAs []string) map[string]string
	// RefDecorations returns local branch and tag refs grouped by the commit SHA
	// they point at (annotated tags dereferenced), for git-log-style annotations.
	RefDecorations() (map[string][]git.RefDecoration, error)
	// GetAllBranchNames returns the names of all local branches, including ones
	// not tracked by stackit. Used by diagnostics that must see untracked or
	// orphaned branches.
	GetAllBranchNames(ctx context.Context) ([]string, error)
	// FindNearestNonExcludedAncestor walks the parent chain from startParent
	// and returns the first ancestor for which isExcluded returns false. Falls
	// back to trunk if every ancestor up the chain is excluded.
	FindNearestNonExcludedAncestor(startParent string, isExcluded func(name string) bool) string
	ValidateOnBranch() (string, error)
	IsBranchEmpty(ctx context.Context, branchName string) (bool, error)
	GetScope(branch Branch) Scope
	GetRemote() string
	GetRepoInfo(ctx context.Context) (string, string, error)
	GetRepoRoot() string
	GetUserName(ctx context.Context) (string, error)
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
	// BatchIsBranchEmpty reports emptiness for many branches, resolving all tree
	// SHAs in one batched rev-parse instead of a diff per branch.
	BatchIsBranchEmpty(branchNames []string) BranchNameSet
	GetDeletionStatuses(ctx context.Context, branchNames []string) (DeletionStatuses, error)
	GetScope(branch Branch) Scope
	GetStackDescription(branch Branch) *git.StackDescription
	IsLocked(branch Branch) bool
	GetLockReason(branch Branch) LockReason
	IsFrozen(branch Branch) bool
	IsWorktreeAnchor(branch Branch) bool
	GetBranchType(branch Branch) git.BranchType
	GetPrInfo(branch Branch) (*PrInfo, error)
	// BatchGetPRSubmissionStatus returns submission status for many branches,
	// reading remote status once for the whole set instead of per branch. The
	// context bounds the remote read (a `git ls-remote`); pass a deadline-bearing
	// context so a stalled remote can't outlive the caller's intent.
	BatchGetPRSubmissionStatus(ctx context.Context, branches Branches) (map[string]PRSubmissionStatus, error)
	// BatchGetPRSubmissionStatusWithRemote is BatchGetPRSubmissionStatus with a
	// caller-supplied remote-status snapshot, so the remote read can be shared.
	BatchGetPRSubmissionStatusWithRemote(branches Branches, remoteStatuses BranchRemoteStatuses) (map[string]PRSubmissionStatus, error)
	FindMostRecentTrackedAncestors(ctx context.Context, branchName string) ([]string, error)
	GetRemote() string
	GetRemoteURL(ctx context.Context) (string, error)
	ReadBranchRemoteStatuses(ctx context.Context, branches Branches) BranchRemoteStatuses
	// TrunkRemoteState reports how the local trunk relates to its
	// remote-tracking branch using only local refs (no network).
	TrunkRemoteState(ctx context.Context) TrunkRemoteState
	GetMergedBranches(ctx context.Context, target string) (BranchNameSet, error)
}

// BranchInfo provides commit and diff metadata
type BranchInfo interface {
	GetCommitDate(branch Branch) (time.Time, error)
	GetCommitAuthor(branch Branch) (string, error)
	// BatchCommitInfo resolves each branch's tip commit date and author in one
	// batched pass instead of two `git log` processes per branch.
	BatchCommitInfo(branches Branches) map[string]git.CommitInfo
	GetRevision(branch Branch) (string, error)
	GetAllCommits(branch Branch, format CommitFormat) ([]string, error)
	GetParentCommitSHA(commitSHA string) (string, error)
	GetCommitSHA(branchName string, offset int) (string, error)
	GetRevisionForName(branchName string) (string, error)
	GetRevisions(branchNames []string) (RevisionMap, []error)
	GetCurrentRevision(ctx context.Context) (string, error)
	GetRecentTrunkCommits(count int) ([]git.RecentCommit, error)
	GetTrunkCommitsInRange(rr git.RevRange) ([]git.RecentCommit, error)
	GetReflog(ctx context.Context, count int, format string) (string, error)
	// GetDivergencePoint returns the divergence point of a branch from its parent.
	// Returns the ParentBranchRevision from metadata if valid, otherwise the parent's current revision.
	GetDivergencePoint(branchName string) (string, error)
	// BatchDivergencePoints returns the divergence point for every branch in one
	// batched (git-free when metadata is cached) pass, keyed by branch name.
	BatchDivergencePoints(branches Branches) RevisionMap
	// BatchDiffStats, BatchCommits, and BatchChangedFileCounts each resolve one
	// per-branch concern across the whole set in a single batched pass, returning
	// a value map.
	BatchDiffStats(branches Branches) map[string]DiffStat
	BatchCommits(branches Branches, format CommitFormat) map[string][]string
	BatchChangedFileCounts(ctx context.Context, branches Branches) map[string]int
	// BatchBranchStats resolves annotation stats (short SHA, commit count,
	// additions/deletions) for every branch in one batched pass — a use-case
	// bundle over the per-concern readers, for annotation builders.
	BatchBranchStats(branches Branches) map[string]BranchStat
}

// GitDiffer handles diff and merge operations
type GitDiffer interface {
	GetMergeBase(ctx context.Context, rev1, rev2 string) (string, error)
	GetChangedFiles(ctx context.Context, rr git.RevRange) ([]string, error)
	IsDiffEmpty(ctx context.Context, base, head string) (bool, error)
	ShowDiff(ctx context.Context, left, right string, stat bool) (string, error)
	ShowCommits(ctx context.Context, rr git.RevRange, patch, stat bool) (string, error)
	IsAncestor(ctx context.Context, ancestor, descendant string) (bool, error)
	// GetDiffBetween returns raw diff between two refs, suitable for parsing into hunks.
	GetDiffBetween(ctx context.Context, rr git.RevRange, files ...string) (string, error)
}

// WorkingTree handles worktree and staging area operations
type WorkingTree interface {
	HasStagedChanges(ctx context.Context) (bool, error)
	HasUnstagedChanges(ctx context.Context) (bool, error)
	HasUntrackedFiles(ctx context.Context) (bool, error)
	GetUntrackedFiles(ctx context.Context) ([]string, error)
	// GetWorkingTreeStatus returns all three working-tree flags in one git call.
	// Prefer this over calling Has* individually when multiple flags are needed.
	GetWorkingTreeStatus(ctx context.Context) (staged, unstaged, untracked bool, err error)
	GetUnstagedDiff(ctx context.Context, files ...string) (string, error)
	// GetUnstagedDiffBinary is like GetUnstagedDiff but includes full binary
	// content (`git diff --binary`) so the result can be reapplied with
	// `git apply`.
	GetUnstagedDiffBinary(ctx context.Context, files ...string) (string, error)
	GetUntrackedFileHunks(ctx context.Context) ([]git.Hunk, error)
	GetPendingChanges(ctx context.Context) ([]PendingChange, error)
	GetCommitTemplate(ctx context.Context) (string, error)
	GetUnmergedFiles(ctx context.Context) ([]string, error)
	ParseStagedHunks(ctx context.Context) ([]git.Hunk, error)
	ListWorktrees(ctx context.Context) (git.WorktreeList, error)
	IsRebaseInProgress(ctx context.Context) bool
	IsMergeInProgress(ctx context.Context) bool
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
	// ReparentBranchesToParents reparents each branch onto its own designated
	// parent (per-branch, unlike ReparentBranches) while preserving divergence
	// points, all captured before any mutation begins.
	ReparentBranchesToParents(ctx context.Context, moves []BranchParentMove) error
	// ReparentBranchesRecompute reparents multiple branches onto the same new
	// parent and recomputes each divergence point against it (fresh merge-base).
	// Use when moving branches under a newly created parent.
	ReparentBranchesRecompute(ctx context.Context, branchNames []string, newParent Branch) error
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
	SoftReset(ctx context.Context, revision string) error
	Merge(ctx context.Context, revision string, opts MergeOptions) error
	MergeMultiple(ctx context.Context, branches []string, opts MergeOptions) error
	Fetch(ctx context.Context, remote string, branch string) error
	InteractiveRebase(ctx context.Context, onto string) error
	RebaseAbort(ctx context.Context) error
	MergeAbort(ctx context.Context) error
}

// CommitOperations handles staging and committing
type CommitOperations interface {
	Commit(ctx context.Context, message string, verbose int, noVerify bool) error
	CommitWithOptions(ctx context.Context, opts git.CommitOptions) error
	StageAll(ctx context.Context) error
	StagePatch(ctx context.Context) error
	StageHunks(ctx context.Context, hunks []git.Hunk) error
	// ApplyPatchToWorktree applies a patch to the working tree only (not the
	// index). It applies atomically or fails without writing conflict markers.
	ApplyPatchToWorktree(ctx context.Context, patch string) error
	StageChanges(ctx context.Context, opts git.StagingOptions) error
	StashPush(ctx context.Context, message string) (string, error)
	StashPushStaged(ctx context.Context, message string) (string, error)
	StashDrop(ctx context.Context, ref string) error
	StashPop(ctx context.Context) error
	StashPopRef(ctx context.Context, ref string) error
}

// WorktreeOperations handles worktree management
type WorktreeOperations interface {
	AddWorktree(ctx context.Context, path string, branch string, detach git.WorktreeDetachMode) error
	RemoveWorktree(ctx context.Context, path string) error
	ForceRemoveWorktree(ctx context.Context, path string) error
	GetWorktreeCurrentBranch(ctx context.Context, worktreePath string) (string, error)
	WorktreeHasUncommittedChanges(ctx context.Context, worktreePath string) (bool, error)
	WorktreeHasTrackedChanges(ctx context.Context, worktreePath string) (bool, error)
	ListIgnoredFiles(ctx context.Context, worktreePath string) ([]string, error)
	CreateTemporaryWorktree(ctx context.Context, branch string, prefix string) (path string, cleanup func(), err error)
	// CreateTemporaryWorktreeSkipPrune is like CreateTemporaryWorktree but skips the automatic
	// PruneWorktrees() call. Use this when creating multiple worktrees in parallel after
	// manually calling PruneWorktrees() once, to avoid race conditions.
	CreateTemporaryWorktreeSkipPrune(ctx context.Context, branch string, prefix string) (path string, cleanup func(), err error)
	PruneWorktrees(ctx context.Context) error
	// PruneOrphanedWorktreePathRefs removes stale reverse registration refs.
	PruneOrphanedWorktreePathRefs(ctx context.Context) (int, error)
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
	// OwningWorktree returns the managed worktree that owns branch's stack, or
	// nil when the branch belongs to an ordinary stack in the main repository.
	// A malformed registration is returned as an error rather than being treated
	// as unowned, so callers can fail closed before mutating the stack.
	OwningWorktree(branch Branch) (*WorktreeInfo, error)
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
	// RebuildBranches refreshes cached state for only the named branches,
	// merging into the existing graph instead of re-reading every branch.
	RebuildBranches(branchNames []string) error
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

// MetadataInspector exposes raw, below-abstraction reads of the stackit branch
// metadata-ref store. It is the low-level escape valve for diagnostic and
// repair commands (doctor, debug) that must observe metadata the engine's
// tracked-branch view cannot see — orphaned, corrupted, or untracked-branch
// refs. Prefer the higher-level branch accessors for normal flows; reach for
// this only when raw ref access is genuinely required.
type MetadataInspector interface {
	// ListMetadataRefs returns a map of branch name to metadata-ref SHA for
	// every stackit metadata ref, including refs whose branches no longer exist.
	ListMetadataRefs() (map[string]string, error)
	// ReadMetadataRaw reads a single branch's metadata directly from its ref,
	// bypassing the engine's tracked-branch cache.
	ReadMetadataRaw(branchName string) (*git.Meta, error)
	// BatchReadMetadataRaw reads raw metadata for many branches in one pass,
	// returning per-branch errors so callers can detect corrupted refs.
	BatchReadMetadataRaw(branchNames []string) (MetaMap, map[string]error)
	// DeleteMetadataRef deletes a single branch's metadata ref directly, without
	// the transactional rebuild performed by DeleteMetadata. Intended for
	// pruning orphaned refs whose branches no longer exist.
	DeleteMetadataRef(ctx context.Context, branchName string) error
}

// GitConfig provides access to git configuration values. Exposed so helpers
// that only need config access (e.g. rerere setup) can take the engine instead
// of the raw git runner.
type GitConfig interface {
	GetConfig(key string) (string, error)
	SetConfig(key, value string) error
}

// Absorber applies staged hunks to appropriate commits
type Absorber interface {
	ApplyHunksToBranch(ctx context.Context, branch Branch, hunksByCommit map[string][]git.Hunk) error
	FindTargetCommitForHunk(hunk git.Hunk, commitSHAs []string) (string, int, error)
}
