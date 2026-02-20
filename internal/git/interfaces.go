package git

import (
	"context"
	"time"
)

// RepositoryReader provides read access to repository configuration and state.
type RepositoryReader interface {
	GetRemote() string
	GetConfig(key string) (string, error)
	GetConfigAll(key string) ([]string, error)
	GetRepoRoot() string
	DiscoverRepoRoot() (string, error)
	// GetGitCommonDir returns the path to the shared .git directory.
	// For regular repos this is the same as .git, but for worktrees it returns
	// the main repository's .git directory (where config is stored).
	GetGitCommonDir() (string, error)
	IsInsideRepo() bool
	GetUserName(ctx context.Context) (string, error)
	GetRepoInfo(ctx context.Context) (RemoteRepository, error)
}

// RepositoryWriter provides write access to repository configuration.
type RepositoryWriter interface {
	InitDefaultRepo() error
	SetConfig(key, value string) error
	AddConfigValue(key, value string) error
	EnsureMetadataRefspecConfigured() error
	EnsureStackMetaRefspecConfigured() error
}

// RemoteOperations handles interaction with remote repositories.
type RemoteOperations interface {
	FetchRemoteShas(ctx context.Context, remote string) (map[string]string, error)
	GetRemoteSha(remote, branchName string) (string, error)
	GetRemoteRevision(branchName string) (string, error)
	FindRemoteBranch(ctx context.Context, remote string) (string, error)
	PushBranch(ctx context.Context, branchName, remote string, opts PushOptions) error
	PushBranches(ctx context.Context, remote string, specs []PushSpec, opts PushOptions) map[string]error
	PullBranch(ctx context.Context, remote, branchName string) (PullResult, error)
	UpdateBranchFromRemote(ctx context.Context, remote, branchName string) (PullResult, error)
	Fetch(ctx context.Context, remote, branch string) error
	FetchRefSpecs(ctx context.Context, remote string, refspecs []string) error
	PushMetadataRefs(ctx context.Context, branches []string) error
	FetchMetadataRefs(ctx context.Context) error
	DeleteRemoteMetadataRef(ctx context.Context, branch string) error
	BatchDeleteRemoteMetadataRefs(ctx context.Context, branches []string) error
	TestRemoteRefCompatibility(ctx context.Context) error
	PushStackMetaRefs(ctx context.Context, stackIDs []string) error
	FetchStackMetaRefs(ctx context.Context) error
	DeleteRemoteStackMetaRefs(ctx context.Context, stackIDs []string) error
}

// BranchReader provides read access to branch information.
type BranchReader interface {
	GetCurrentBranch() (string, error)
	GetAllBranchNames(ctx context.Context) ([]string, error)
	GetCurrentBranchOrSHA(ctx context.Context) (string, error)
}

// BranchWriter handles branch lifecycle operations.
type BranchWriter interface {
	CheckoutBranch(ctx context.Context, branchName string) error
	CheckoutBranchForce(ctx context.Context, branchName string) error
	CheckoutDetached(ctx context.Context, revision string) error
	CreateAndCheckoutBranch(ctx context.Context, branchName string) error
	CreateBranch(ctx context.Context, branchName, startPoint string) error
	CreateBranchForce(ctx context.Context, branchName, revision string) error
	DeleteBranch(ctx context.Context, branchName string) error
	RenameBranch(ctx context.Context, oldName, newName string) error
	UpdateBranchRef(ctx context.Context, branchName, revision string) error
	// UpdateBranchRefCAS moves a branch only if it still names expectedOld.
	// Callers that prepared commits from a detached snapshot use this to avoid
	// overwriting concurrent work from another worktree.
	UpdateBranchRefCAS(ctx context.Context, branchName, revision, expectedOld string) error
}

// CommitReader provides read access to commit and revision information.
type CommitReader interface {
	GetRevision(branchName string) (string, error)
	GetCurrentRevision(ctx context.Context) (string, error)
	BatchGetRevisions(branchNames []string) (map[string]string, []error)
	GetCommitDate(branchName string) (time.Time, error)
	GetCommitAuthor(branchName string) (string, error)
	// BatchCommitInfo resolves each branch's tip commit date and author in one
	// `git for-each-ref` invocation instead of two `git log` processes per branch.
	BatchCommitInfo(branchNames []string) map[string]CommitInfo
	GetCommitRange(ctx context.Context, base, head, format string) ([]string, error)
	GetCommitRangeSHAs(ctx context.Context, rr RevRange) ([]string, error)
	GetCommitHistorySHAs(ctx context.Context, branchName string) ([]string, error)
	GetCommitSHA(branchName string, offset int) (string, error)
	GetCommitLog(sha, format string) (string, error)
	GetRecentCommits(ctx context.Context, branchName string, count int) ([]RecentCommit, error)
	GetRecentCommitsInRange(ctx context.Context, revRange string) ([]RecentCommit, error)
	GetCommitTemplate(ctx context.Context) (string, error)
	GetParentCommitSHA(commitSHA string) (string, error)
}

// DiffOperations provides access to diff and comparison operations.
type DiffOperations interface {
	GetMergeBase(ctx context.Context, rev1, rev2 string) (string, error)
	GetMergeBaseByRef(ctx context.Context, ref1, ref2 string) (string, error)
	IsAncestor(ctx context.Context, ancestor, descendant string) (bool, error)
	IsMerged(ctx context.Context, branchName, target string) (bool, error)
	IsSquashMerged(ctx context.Context, branchName, target string, cache *SquashMergeCache) (bool, error)
	GetMergedBranches(ctx context.Context, target string) (map[string]bool, error)
	IsDiffEmpty(ctx context.Context, branchName, base string) (bool, error)
	GetChangedFiles(ctx context.Context, rr RevRange) ([]string, error)
	ShowDiff(ctx context.Context, left, right string, stat bool) (string, error)
	ShowCommits(ctx context.Context, rr RevRange, patch, stat bool) (string, error)
	GetDiffNumstat(rr RevRange) (string, error)
	GetStagedDiff(ctx context.Context, files ...string) (string, error)
	GetUnstagedDiff(ctx context.Context, files ...string) (string, error)
	// GetUnstagedDiffBinary is like GetUnstagedDiff but includes full binary
	// content (`git diff --binary`) so the result can be reapplied with
	// `git apply`.
	GetUnstagedDiffBinary(ctx context.Context, files ...string) (string, error)
	// GetDiffBetween returns the raw diff between two refs, without color codes.
	// This is suitable for parsing into hunks.
	GetDiffBetween(ctx context.Context, rr RevRange, files ...string) (string, error)
}

// StagingOperations handles staging area operations.
type StagingOperations interface {
	StageAll(ctx context.Context) error
	StagePatch(ctx context.Context) error
	StageTracked(ctx context.Context) error
	AddAll(ctx context.Context) error
	StageChanges(ctx context.Context, opts StagingOptions) error
	HasStagedChanges(ctx context.Context) (bool, error)
	HasUnstagedChanges(ctx context.Context) (bool, error)
	HasUntrackedFiles(ctx context.Context) (bool, error)
	GetUntrackedFiles(ctx context.Context) ([]string, error)
	ParseStagedHunks(ctx context.Context) ([]Hunk, error)
	StageHunks(ctx context.Context, hunks []Hunk) error
	UnstageAll(ctx context.Context) error
}

// CommitWriter handles commit creation and modification.
type CommitWriter interface {
	Commit(message string, verbose int, noVerify bool) error
	CommitWithOptions(opts CommitOptions) error
	CommitAmendNoEdit(ctx context.Context) error
}

// RebaseOperations handles rebase operations.
type RebaseOperations interface {
	Rebase(ctx context.Context, branchName, upstream, oldUpstream string) (RebaseOutcome, error)
	RebaseContinue(ctx context.Context) (RebaseOutcome, error)
	RebaseContinueNoEdit(ctx context.Context) (RebaseOutcome, error)
	RebaseAbort(ctx context.Context) error
	InteractiveRebase(ctx context.Context, onto string) error
	IsRebaseInProgress(ctx context.Context) bool
	GetRebaseHead() (string, error)
	CheckRebaseInProgress(ctx context.Context) error
}

// MergeOperations handles merge operations.
type MergeOperations interface {
	Merge(ctx context.Context, branchName string, opts MergeOptions) error
	MergeMultiple(ctx context.Context, branches []string, opts MergeOptions) error
	IsMergeInProgress(ctx context.Context) bool
	MergeAbort(ctx context.Context) error
	GetUnmergedFiles(ctx context.Context) ([]string, error)
}

// CherryPickOperations handles cherry-pick operations.
type CherryPickOperations interface {
	CherryPick(ctx context.Context, commitSHA, onto string) (string, error)
	CherryPickSimple(ctx context.Context, commitSHA string) error
	CherryPickAbort(ctx context.Context) error
}

// StashOperations handles stash operations.
type StashOperations interface {
	StashPush(ctx context.Context, message string) (string, error)
	StashPushStaged(ctx context.Context, message string) (string, error)
	StashDrop(ctx context.Context, ref string) error
	StashPop(ctx context.Context) error
	StashPopRef(ctx context.Context, ref string) error
	ListStash(ctx context.Context) (string, error)
}

// ResetOperations handles reset operations.
type ResetOperations interface {
	HardReset(ctx context.Context, revision string) error
	ResetMerge(ctx context.Context, revision string) error
	SoftReset(ctx context.Context, revision string) error
	MixedReset(ctx context.Context, revision string) error
}

// PathOperations handles file path operations.
type PathOperations interface {
	CheckoutPaths(ctx context.Context, branch string, paths []string) error
	RemovePaths(ctx context.Context, paths []string) error
}

// PatchOperations handles patch operations.
type PatchOperations interface {
	ApplyPatch(ctx context.Context, patchFile string, threeWay bool) error
	// ApplyPatchToWorktree applies a patch (read from stdin) to the working
	// tree only, never the index. It applies atomically or fails leaving the
	// working tree untouched, so it can never write conflict markers.
	ApplyPatchToWorktree(ctx context.Context, patch string) error
	CheckCommutation(hunk Hunk, commitSHA, parentSHA string) (bool, error)
}

// WorktreeOperations handles worktree management.
type WorktreeOperations interface {
	AddWorktree(ctx context.Context, path string, branch string, detach WorktreeDetachMode) error
	AddWorktreeWithOptions(ctx context.Context, path string, branch string, detach WorktreeDetachMode, noCheckout bool) error
	RemoveWorktree(ctx context.Context, path string) error
	ForceRemoveWorktree(ctx context.Context, path string) error
	ListWorktrees(ctx context.Context) (WorktreeList, error)
	PruneWorktrees(ctx context.Context) error
	GetWorktreePathForBranch(ctx context.Context, branchName string) (string, error)
	GetWorktreeCurrentBranch(ctx context.Context, worktreePath string) (string, error)
	ResetWorktreeWorkingDir(ctx context.Context, worktreePath string) error
	WorktreeHasUncommittedChanges(ctx context.Context, worktreePath string) (bool, error)
	WorktreeHasTrackedChanges(ctx context.Context, worktreePath string) (bool, error)
	// WorktreeResetBlocker reports why resetting worktreePath to incomingRev
	// would destroy work, or "" when it is safe.
	WorktreeResetBlocker(ctx context.Context, worktreePath, incomingRev string) string
	// ListIgnoredFiles returns repository-relative paths that Git currently
	// classifies as ignored in the requested worktree.
	ListIgnoredFiles(ctx context.Context, worktreePath string) ([]string, error)
}

// WorktreeRegistryOperations handles stackit-managed worktree tracking (local-only refs).
type WorktreeRegistryOperations interface {
	ReadWorktreeMeta(stackRoot string) (*WorktreeMeta, error)
	WriteWorktreeMeta(ctx context.Context, stackRoot string, meta *WorktreeMeta) error
	DeleteWorktreeMeta(ctx context.Context, stackRoot string) error
	ListWorktreeMetas() (map[string]*WorktreeMeta, error)
}

// StatusOperations provides repository status information.
type StatusOperations interface {
	GetStatusPorcelain(ctx context.Context) (string, error)
	GetReflog(ctx context.Context, count int, format string) (string, error)
	HasUncommittedChanges(ctx context.Context) bool
}

// RefOperations provides low-level reference operations.
type RefOperations interface {
	GetRef(name string) (string, error)
	UpdateRef(name, sha string) error
	UpdateRefWithLog(ctx context.Context, refName, sha, message string) error
	GetUntrackedFilesIn(ctx context.Context, worktreePath string) ([]string, error)
	TreeContainsAnyPath(ctx context.Context, rev string, paths []string) (collides, known bool)
	UpdateRefsBatch(ctx context.Context, updates []RefUpdate) error
	UpdateRefsBatchWithLog(ctx context.Context, updates []RefUpdate, reflogMessage string) error
	DeleteRefsBatch(ctx context.Context, refNames []string) error
	VerifyRef(ctx context.Context, refName string) error
	DeleteRef(ctx context.Context, name string) error
	ListRefs(prefix string) (map[string]string, error)
	// RefDecorations returns local branch and tag refs grouped by the commit SHA
	// they point at, dereferencing annotated tags to the wrapped commit.
	RefDecorations() (map[string][]RefDecoration, error)
}

// ObjectOperations provides low-level Git object operations.
type ObjectOperations interface {
	CreateBlob(content string) (string, error)
	// CreateBlobsBatch writes N blobs in a single `git hash-object` invocation.
	// Returns SHAs in input order. For small N (<3) callers should still use
	// CreateBlob — the temp-file staging required by the batch path only pays
	// off once per-blob subprocess overhead would dominate. ctx is honored for
	// the underlying git invocation so long-running batches can be canceled.
	CreateBlobsBatch(ctx context.Context, contents []string) ([]string, error)
	ReadBlob(sha string) (string, error)
	CatFile(sha string) (string, error)
}

// MetadataOperations handles stackit metadata persistence.
type MetadataOperations interface {
	ReadMetadata(branchName string) (*Meta, error)
	BatchReadMetadata(branchNames []string) (map[string]*Meta, map[string]error)
	WriteMetadata(branchName string, meta *Meta) error
	DeleteMetadata(ctx context.Context, branchName string) error
	RenameMetadata(oldName, newName string) error
	ListMetadata() (map[string]string, error)
	ReadLocalMetadata(branchName string) (*LocalMeta, error)
	BatchReadLocalMetadata(branchNames []string) LocalMetaMap
	WriteLocalMetadata(branchName string, meta *LocalMeta) error

	// Transaction support methods. The batch forms marshal each entry and
	// forward to CreateBlobsBatch — call them with len(metas) >= 1 from
	// engine_writer.go and transaction.go's commit path. ctx is honored for
	// the underlying git hash-object invocation.
	WriteMetadataBlobsBatch(ctx context.Context, metas []*Meta) ([]string, error)
	WriteLocalMetadataBlobsBatch(ctx context.Context, metas []*LocalMeta) ([]string, error)
	GetMetadataRefSHA(branchName string) string
	GetLocalMetadataRefSHA(branchName string) string

	// Cache management
	ClearMetadataCache()

	// MetadataCacheStats returns cumulative cache hit/miss counts since process start.
	// Used by tests and instrumentation to verify lazy-load behavior.
	MetadataCacheStats() MetadataCacheSummary
}

// NotesOperations handles prompt note persistence via git notes.
// Notes are stored on individual commits using refs/notes/prompts.
type NotesOperations interface {
	AddPromptNote(ctx context.Context, commit string, note *PromptNote) error
	ShowPromptNote(ctx context.Context, commit string) (*PromptNote, error)
	RemovePromptNote(ctx context.Context, commit string) error
	LogWithNotes(ctx context.Context, base, head string) ([]NoteEntry, error)
	PushNotes(ctx context.Context) error
	FetchNotes(ctx context.Context) error
	EnsureNotesRewriteConfigured() error
	EnsureNotesRefspecConfigured() error
}

// StackMetadataOperations handles stack-level metadata persistence.
// Stack metadata is stored separately from branch metadata and survives branch operations.
type StackMetadataOperations interface {
	ReadStackMeta(stackID string) (*StackMeta, error)
	WriteStackMeta(stackID string, meta *StackMeta) error
	DeleteStackMeta(ctx context.Context, stackID string) error
	ListStackMetas() (map[string]string, error)

	// Transaction support methods
	WriteStackMetaBlob(meta *StackMeta) (string, error)
	GetStackMetaRefSHA(stackID string) string
}
