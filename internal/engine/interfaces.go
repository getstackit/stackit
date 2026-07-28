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
	FindBranchesForCommits(commitSHAs []string) CommitBranchMap
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
	GetScope(branch Branch) Scope
	GetRemote() string
	GetRepoInfo(ctx context.Context) (git.RemoteRepository, error)
	GetRepoRoot() string
	GetUserName(ctx context.Context) (string, error)
	// GitVersion reports the installed Git's version, for diagnostics.
	GitVersion(ctx context.Context) (git.Version, error)
	IsInsideRepo() bool
}

// BranchStatus provides branch state information
type BranchStatus interface {
	IsTrunk(branch Branch) bool
	IsTracked(branch Branch) bool
	IsUpToDate(branch Branch) bool
	ReadBranchStatuses(branches Branches) BranchStatuses
	IsBranchEmpty(ctx context.Context, branchName string) (bool, error)
	// BatchIsBranchEmpty reports emptiness for many branches, resolving all tree
	// SHAs in one batched rev-parse instead of a diff per branch.
	BatchIsBranchEmpty(branchNames []string) BranchNameSet
	GetDeletionStatuses(ctx context.Context, branchNames []string) (DeletionStatuses, error)
	// GetStackDescription returns the stack description for a branch's stack.
	// It first checks the stack ref, then falls back to legacy branch metadata.
	GetStackDescription(branch Branch) *git.StackDescription
	IsLocked(branch Branch) bool
	GetLockReason(branch Branch) LockReason
	IsFrozen(branch Branch) bool
	IsWorktreeAnchor(branch Branch) bool
	GetBranchType(branch Branch) git.BranchType
	GetPrInfo(branch Branch) (*PrInfo, error)
	// BatchGetPRSubmissionStatus returns submission status for many branches.
	// Pass a remote-status snapshot to share an earlier remote read, or nil to
	// read remote status at most once for the whole set. The context bounds
	// that read (a `git ls-remote`); pass a deadline-bearing context so a
	// stalled remote can't outlive the caller's intent.
	BatchGetPRSubmissionStatus(ctx context.Context, branches Branches, remoteStatuses BranchRemoteStatuses) (map[string]PRSubmissionStatus, error)
	FindMostRecentTrackedAncestors(ctx context.Context, branchName string) ([]string, error)
	GetRemoteURL(ctx context.Context) (string, error)
	ReadBranchRemoteStatuses(ctx context.Context, branches Branches) BranchRemoteStatuses
	// MissingRemoteBranches returns the subset of the given branches that no
	// longer exist on the configured remote.
	MissingRemoteBranches(ctx context.Context, branchNames []string) ([]string, error)
	// TrunkRemoteState reports how the local trunk relates to its
	// remote-tracking branch using only local refs (no network).
	TrunkRemoteState(ctx context.Context) TrunkRemoteState
}

// BranchInfo provides commit and diff metadata
type BranchInfo interface {
	GetCommitDate(branch Branch) (time.Time, error)
	// BatchCommitInfo resolves each branch's tip commit date and author in one
	// batched pass instead of two `git log` processes per branch.
	BatchCommitInfo(branches Branches) map[string]git.CommitInfo
	GetRevision(branch Branch) (string, error)
	GetAllCommits(branch Branch) (git.Commits, error)
	GetCommitIDs(branch Branch) ([]string, error)
	GetCommitSHA(branchName string, offset int) (string, error)
	// BatchRevisions resolves every branch's tip SHA in one batched pass,
	// keyed by branch name.
	BatchRevisions(branches Branches) RevisionMap
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
	BatchCommits(branches Branches) map[string]git.Commits
	ReadBranchCommits(ctx context.Context, branches Branches) git.ReadResults[BranchCommitRange]
	ReadBranchCommitNodes(ctx context.Context, branches Branches) git.ReadResults[[]git.CommitNode]
	BatchChangedFileCounts(ctx context.Context, branches Branches) map[string]int
	// BatchBranchStats resolves annotation stats (short SHA, commit count,
	// additions/deletions) for every branch in one batched pass — a use-case
	// bundle over the per-concern readers, for annotation builders.
	BatchBranchStats(branches Branches) map[string]BranchStat
	// CommitCountBetween returns how many commits are in (base, head], for
	// callers holding two plain revisions rather than a branch set — e.g.
	// reporting how far trunk moved during a sync.
	CommitCountBetween(ctx context.Context, rr git.RevRange) (int, error)
}

// GitDiffer handles diff and merge operations
type GitDiffer interface {
	GetMergeBase(ctx context.Context, rev1, rev2 string) (string, error)
	GetChangedFiles(ctx context.Context, rr git.RevRange) ([]string, error)
	ShowDiff(ctx context.Context, rr git.RevRange, format git.DiffFormat) (string, error)
	ShowCommits(ctx context.Context, rr git.RevRange, format git.CommitLogFormat) (string, error)
	IsAncestor(ctx context.Context, ancestor, descendant string) (bool, error)
	// GetDiffBetween returns raw diff between two refs, suitable for parsing into hunks.
	GetDiffBetween(ctx context.Context, rr git.RevRange, files ...string) (string, error)
}

// WorkingTree handles worktree and staging area operations
type WorkingTree interface {
	HasStagedChanges(ctx context.Context) (bool, error)
	HasUnstagedChanges(ctx context.Context) (bool, error)
	GetUntrackedFiles(ctx context.Context) ([]string, error)
	// GetWorkingTreeStatus returns all three working-tree flags in one git call.
	// Prefer this over calling Has* individually when multiple flags are needed.
	GetWorkingTreeStatus(ctx context.Context) (git.WorktreeStatus, error)
	GetUnstagedDiff(ctx context.Context, files ...string) (string, error)
	// GetUnstagedDiffBinary is like GetUnstagedDiff but includes full binary
	// content (`git diff --binary`) so the result can be reapplied with
	// `git apply`.
	GetUnstagedDiffBinary(ctx context.Context, files ...string) (string, error)
	GetUntrackedFileHunks(ctx context.Context) ([]git.Hunk, error)
	GetCommitTemplate(ctx context.Context) (string, error)
	GetUnmergedFiles(ctx context.Context) ([]string, error)
	ParseStagedHunks(ctx context.Context) ([]git.Hunk, error)
	ListWorktrees(ctx context.Context) (git.WorktreeList, error)
	IsRebaseInProgress(ctx context.Context) bool
	IsMergeInProgress(ctx context.Context) bool
	GetRebaseHead() (string, error)
	HasUncommittedChanges(ctx context.Context) bool
	CheckoutPaths(ctx context.Context, branch string, pathspecs []string) error
	StashList(ctx context.Context) (string, error)
}

// StackView is the narrow read surface for consumers that display or analyze
// stack structure (tree renderers, API view assembly, shippability analysis):
// branch lookup, the stack graph, scopes, and batched restack/remote status.
// Prefer it (or a narrower local interface) over BranchReader for read-only
// adapters.
type StackView interface {
	Trunk() Branch
	CurrentBranch() *Branch
	CurrentBranchName() string
	GetBranch(branchName string) Branch
	AllBranches() Branches
	Graph(strategy SortStrategy) *StackGraph
	BranchesDepthFirst(startBranch Branch) iter.Seq2[Branch, int]
	GetScope(branch Branch) Scope
	ReadBranchStatuses(branches Branches) BranchStatuses
	ReadBranchRemoteStatuses(ctx context.Context, branches Branches) BranchRemoteStatuses
}

// BranchLookup resolves branches by name.
type BranchLookup interface {
	GetBranch(branchName string) Branch
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
	// TrackBranchPastLandedParent tracks a branch under a substitute parent
	// when the parent it was built on is gone locally, using the recorded
	// prior parent to anchor the divergence point so a later restack does not
	// replay landed commits.
	TrackBranchPastLandedParent(ctx context.Context, branchName string, parentBranchName string, prior PriorParent) error
	// UntrackBranches stops tracking multiple branches, deleting their metadata
	// and triggering a single engine rebuild.
	UntrackBranches(ctx context.Context, branchNames []string) error
	SetParent(ctx context.Context, branch Branch, parentBranch Branch, mode DivergenceMode) error
	// ReparentBranch changes a branch's parent while automatically preserving
	// its divergence point. Preferred over SetParent for existing branches.
	ReparentBranch(ctx context.Context, branch Branch, newParent Branch) error
	// ReparentBranchesToParents reparents each branch onto its own designated
	// parent (MovesTo builds moves onto one shared parent). opts.Divergence
	// selects preserving each divergence point (captured before any mutation)
	// or recomputing it against the new parent.
	ReparentBranchesToParents(ctx context.Context, moves []BranchParentMove, opts ReparentOpts) error
	// ApplyParentUpdatesAfterRemovals applies parent updates while evaluating
	// linear-stack validation after removed branches have disappeared. Cleanup
	// actions use this before deleting merged parents, so a chain collapse is
	// not mistaken for a temporary fork.
	ApplyParentUpdatesAfterRemovals(ctx context.Context, updates []BranchParentUpdate, removed []string) error
	SetScope(ctx context.Context, branch Branch, scope Scope) error
	// SetScopeAndMarkForUpdate sets the scope and marks the branch as needing a
	// PR body update in one atomic transaction instead of two separate ref writes.
	SetScopeAndMarkForUpdate(ctx context.Context, branch Branch, scope Scope) error
	SetBranchType(branch Branch, branchType git.BranchType) error
	SetLocked(ctx context.Context, branches Branches, reason LockReason) (BatchLockResult, error)
	SetFrozen(ctx context.Context, branches Branches, state FreezeState) (BatchFreezeResult, error)

	// MarkBranchesForPRBodyUpdate marks multiple branches as needing a PR body
	// update in a single atomic operation.
	MarkBranchesForPRBodyUpdate(ctx context.Context, branchNames []string) error
	// ClearNeedsPRBodyUpdate clears the PR body update flag for a branch
	ClearNeedsPRBodyUpdate(branchName string) error
	// GetBranchesNeedingPRBodyUpdate returns all branches that need PR body updates
	GetBranchesNeedingPRBodyUpdate() []string

	// SetStackDescription sets the stack description in the stack ref for a branch.
	// Returns an error if the branch is not part of a tracked stack.
	SetStackDescription(ctx context.Context, branch Branch, desc *git.StackDescription) error
	// ClearStackDescription removes the stack description from the stack ref.
	ClearStackDescription(ctx context.Context, branch Branch) error

	// GetStackID returns the stack ID for a branch.
	// Returns empty string for untracked branches or trunk.
	// For legacy branches without StackID, derives it from the stack root.
	GetStackID(branch Branch) string
	// SetStackID sets the stack ID on multiple branches atomically in a single transaction.
	SetStackID(ctx context.Context, branches Branches, stackID string) error
	// AssignBranchesToNewStack creates a new stack metadata ref and assigns its
	// ID to the provided branches atomically.
	AssignBranchesToNewStack(ctx context.Context, root Branch, branches Branches) (string, error)
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
	SoftReset(ctx context.Context, revision string) error
	Merge(ctx context.Context, revision string, opts MergeOptions) error
	MergeMultiple(ctx context.Context, branches []string, opts MergeOptions) error
	InteractiveRebase(ctx context.Context, onto string) error
	RebaseAbort(ctx context.Context) error
	MergeAbort(ctx context.Context) error
}

// CommitOperations handles staging and committing
type CommitOperations interface {
	Commit(ctx context.Context, opts git.CommitOptions) error
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
	StashPopRef(ctx context.Context, ref string) error
}

// WorktreeOperations handles worktree management
type WorktreeOperations interface {
	AddWorktree(ctx context.Context, path WorktreePath, branch string, detach git.WorktreeDetachMode) error
	RemoveWorktree(ctx context.Context, path WorktreePath) error
	ForceRemoveWorktree(ctx context.Context, path WorktreePath) error
	WorktreeHasUncommittedChanges(ctx context.Context, worktreePath WorktreePath) (bool, error)
	WorktreeHasTrackedChanges(ctx context.Context, worktreePath WorktreePath) (bool, error)
	UntrackedCollision(ctx context.Context, branchName string, worktreePath WorktreePath) (collides, known bool)
	// WorktreeRebaseInProgress reports whether a rebase is active in worktreePath.
	WorktreeRebaseInProgress(ctx context.Context, worktreePath WorktreePath) bool
	ListIgnoredFiles(ctx context.Context, worktreePath WorktreePath) ([]string, error)
	// CreateTemporaryWorktree creates a detached temporary worktree. Pass
	// WorktreePruneSkip when creating several in parallel after calling
	// PruneWorktrees once, to avoid racing on the prune.
	CreateTemporaryWorktree(ctx context.Context, branch string, prefix string, prune WorktreePruneMode) (path WorktreePath, cleanup func(), err error)
	PruneWorktrees(ctx context.Context) error
	// PruneOrphanedWorktreePathRefs removes stale reverse registration refs.
	PruneOrphanedWorktreePathRefs(ctx context.Context) (int, error)
}

// WorktreeName identifies a Stackit-managed worktree's user-facing name. It
// aliases the git-layer type so names flow from stored metadata to actions
// without conversion.
type WorktreeName = git.WorktreeName

// WorktreePath identifies a physical worktree checkout. It aliases the
// git-layer type so paths flow from `git worktree list` and stored metadata
// through the engine and actions without conversion.
type WorktreePath = git.WorktreePath

// WorktreeRegistration identifies the metadata needed to register a managed
// worktree. Keeping these fields together prevents anchor, path, and display
// name from being passed as unrelated primitive arguments.
type WorktreeRegistration struct {
	AnchorBranch string
	Path         WorktreePath
	Name         WorktreeName
}

// WorktreeInfo represents information about a stackit-managed worktree
type WorktreeInfo struct {
	Name         WorktreeName // User-provided name for display
	Path         WorktreePath // Absolute path to worktree
	AnchorBranch string       // Hidden worktree anchor branch name
	CreatedAt    time.Time    // When worktree was created
	MainRepoDir  string       // Path to main repo
}

// DisplayName returns the user-provided worktree name, falling back to its
// anchor when the registration predates named worktrees.
func (w WorktreeInfo) DisplayName() string {
	if w.Name != "" {
		return w.Name.String()
	}
	return w.AnchorBranch
}

// Registration returns the mutable registration identity for this worktree.
func (w WorktreeInfo) Registration() WorktreeRegistration {
	return WorktreeRegistration{
		AnchorBranch: w.AnchorBranch,
		Path:         w.Path,
		Name:         w.Name,
	}
}

// WorktreeRegistry handles stackit-managed worktree tracking
type WorktreeRegistry interface {
	// RegisterWorktree registers a managed worktree.
	RegisterWorktree(ctx context.Context, registration WorktreeRegistration) error
	// UnregisterWorktree removes worktree registration for a stack root
	UnregisterWorktree(ctx context.Context, stackRoot string) error
	// GetWorktreeForStack returns worktree info for a stack root, or nil if none
	GetWorktreeForStack(stackRoot string) (*WorktreeInfo, error)
	// OwningWorktree returns the managed worktree that owns branch's stack, or
	// nil when the branch belongs to an ordinary stack in the main repository.
	// A malformed registration is returned as an error rather than being treated
	// as unowned, so callers can fail closed before mutating the stack.
	OwningWorktree(branch Branch) (*WorktreeInfo, error)
	// WorktreeOwnershipByStackRoot resolves OwningWorktree for every stack
	// root from one listing, with the same fail-closed treatment of malformed
	// registrations.
	WorktreeOwnershipByStackRoot() (map[string]WorktreeOwnership, error)
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
	// DeleteMetadataRefsBatch deletes many branches' metadata refs in a single
	// atomic git call, without the transactional rebuild performed by
	// DeleteMetadata. Intended for pruning refs whose branches no longer exist.
	DeleteMetadataRefsBatch(ctx context.Context, branchNames []string) error
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
