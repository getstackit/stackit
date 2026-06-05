package git

import (
	"context"
	"time"
)

// Runner defines the interface for git operations used by the engine.
// This allows the engine to be used with both real git and mock implementations.
//
// Runner is a single, monolithic surface: every consumer takes the whole
// Runner, so the methods are grouped here by section comment rather than split
// into separate named sub-interfaces (which would imply a narrow-dependency
// decoupling that nothing actually uses). Consumers that only need a slice of
// git behavior should define their own narrow interface at the call site.
type Runner interface {
	// --- Repository access and configuration ---
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
	GetRepoInfo(ctx context.Context) (string, string, error)
	InitDefaultRepo() error
	SetConfig(key, value string) error
	AddConfigValue(key, value string) error
	EnsureMetadataRefspecConfigured() error
	EnsureStackMetaRefspecConfigured() error

	// --- Remote operations ---
	FetchRemoteShas(ctx context.Context, remote string) (map[string]string, error)
	GetRemoteSha(remote, branchName string) (string, error)
	GetRemoteRevision(branchName string) (string, error)
	FindRemoteBranch(ctx context.Context, remote string) (string, error)
	PushBranch(ctx context.Context, branchName, remote string, opts PushOptions) error
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

	// --- Branch read ---
	GetCurrentBranch() (string, error)
	GetAllBranchNames(ctx context.Context) ([]string, error)
	GetCurrentBranchOrSHA(ctx context.Context) (string, error)

	// --- Branch write ---
	CheckoutBranch(ctx context.Context, branchName string) error
	CheckoutBranchForce(ctx context.Context, branchName string) error
	CheckoutDetached(ctx context.Context, revision string) error
	CreateAndCheckoutBranch(ctx context.Context, branchName string) error
	CreateBranch(ctx context.Context, branchName, startPoint string) error
	CreateBranchForce(ctx context.Context, branchName, revision string) error
	DeleteBranch(ctx context.Context, branchName string) error
	RenameBranch(ctx context.Context, oldName, newName string) error
	UpdateBranchRef(ctx context.Context, branchName, revision string) error

	// --- Commit and revision access ---
	GetRevision(branchName string) (string, error)
	GetCurrentRevision(ctx context.Context) (string, error)
	BatchGetRevisions(branchNames []string) (map[string]string, []error)
	// LoadAllBranchRevisions populates the revision cache for all local branches
	// using one `git for-each-ref` invocation. Subsequent GetRevision calls
	// for cached branches resolve in-process without spawning git.
	LoadAllBranchRevisions() error
	GetCommitDate(branchName string) (time.Time, error)
	GetCommitAuthor(branchName string) (string, error)
	GetCommitRange(ctx context.Context, base, head, format string) ([]string, error)
	GetCommitRangeSHAs(ctx context.Context, base, head string) ([]string, error)
	GetCommitHistorySHAs(ctx context.Context, branchName string) ([]string, error)
	GetCommitSHA(branchName string, offset int) (string, error)
	GetCommitLog(sha, format string) (string, error)
	GetRecentCommits(ctx context.Context, branchName string, count int) ([]RecentCommit, error)
	GetCommitTemplate(ctx context.Context) (string, error)
	GetParentCommitSHA(commitSHA string) (string, error)

	// --- Diff and comparison ---
	GetMergeBase(ctx context.Context, rev1, rev2 string) (string, error)
	GetMergeBaseByRef(ctx context.Context, ref1, ref2 string) (string, error)
	IsAncestor(ctx context.Context, ancestor, descendant string) (bool, error)
	IsMerged(ctx context.Context, branchName, target string) (bool, error)
	GetMergedBranches(ctx context.Context, target string) (map[string]bool, error)
	IsDiffEmpty(ctx context.Context, branchName, base string) (bool, error)
	GetChangedFiles(ctx context.Context, base, head string) ([]string, error)
	ShowDiff(ctx context.Context, left, right string, stat bool) (string, error)
	ShowCommits(ctx context.Context, base, head string, patch, stat bool) (string, error)
	GetDiffNumstat(base, head string) (string, error)
	GetStagedDiff(ctx context.Context, files ...string) (string, error)
	GetUnstagedDiff(ctx context.Context, files ...string) (string, error)
	// GetDiffBetween returns the raw diff between two refs, without color codes.
	// This is suitable for parsing into hunks.
	GetDiffBetween(ctx context.Context, base, head string, files ...string) (string, error)

	// --- Staging area ---
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

	// --- Commit creation ---
	Commit(message string, verbose int, noVerify bool) error
	CommitWithOptions(opts CommitOptions) error
	CommitAmendNoEdit(ctx context.Context) error

	// --- Rebase ---
	Rebase(ctx context.Context, branchName, upstream, oldUpstream string) (RebaseOutcome, error)
	RebaseContinue(ctx context.Context) (RebaseOutcome, error)
	RebaseContinueNoEdit(ctx context.Context) (RebaseOutcome, error)
	RebaseAbort(ctx context.Context) error
	InteractiveRebase(ctx context.Context, onto string) error
	IsRebaseInProgress(ctx context.Context) bool
	GetRebaseHead() (string, error)
	CheckRebaseInProgress(ctx context.Context) error

	// --- Merge ---
	Merge(ctx context.Context, branchName string, opts MergeOptions) error
	MergeMultiple(ctx context.Context, branches []string, opts MergeOptions) error
	IsMergeInProgress(ctx context.Context) bool
	MergeAbort(ctx context.Context) error
	GetUnmergedFiles(ctx context.Context) ([]string, error)

	// --- Cherry-pick ---
	CherryPick(ctx context.Context, commitSHA, onto string) (string, error)
	CherryPickSimple(ctx context.Context, commitSHA string) error
	CherryPickAbort(ctx context.Context) error

	// --- Stash ---
	StashPush(ctx context.Context, message string) (string, error)
	StashPushStaged(ctx context.Context, message string) (string, error)
	StashPop(ctx context.Context) error
	ListStash(ctx context.Context) (string, error)

	// --- Reset ---
	HardReset(ctx context.Context, revision string) error
	ResetMerge(ctx context.Context, revision string) error
	SoftReset(ctx context.Context, revision string) error
	MixedReset(ctx context.Context, revision string) error

	// --- File paths ---
	CheckoutPaths(ctx context.Context, branch string, paths []string) error
	RemovePaths(ctx context.Context, paths []string) error

	// --- Patches ---
	ApplyPatch(ctx context.Context, patchFile string, threeWay bool) error
	CheckCommutation(hunk Hunk, commitSHA, parentSHA string) (bool, error)

	// --- Worktree management ---
	AddWorktree(ctx context.Context, path string, branch string, detach bool) error
	AddWorktreeWithOptions(ctx context.Context, path string, branch string, detach bool, noCheckout bool) error
	RemoveWorktree(ctx context.Context, path string) error
	ForceRemoveWorktree(ctx context.Context, path string) error
	ListWorktrees(ctx context.Context) (WorktreeList, error)
	PruneWorktrees(ctx context.Context) error
	GetWorktreePathForBranch(ctx context.Context, branchName string) (string, error)
	GetWorktreeCurrentBranch(ctx context.Context, worktreePath string) (string, error)
	ResetWorktreeWorkingDir(ctx context.Context, worktreePath string) error
	WorktreeHasUncommittedChanges(ctx context.Context, worktreePath string) (bool, error)

	// --- Worktree registry (stackit-managed worktree tracking, local-only refs) ---
	ReadWorktreeMeta(stackRoot string) (*WorktreeMeta, error)
	WriteWorktreeMeta(stackRoot string, meta *WorktreeMeta) error
	DeleteWorktreeMeta(ctx context.Context, stackRoot string) error
	ListWorktreeMetas() (map[string]*WorktreeMeta, error)

	// --- Repository status ---
	GetStatusPorcelain(ctx context.Context) (string, error)
	GetReflog(ctx context.Context, count int, format string) (string, error)
	HasUncommittedChanges(ctx context.Context) bool

	// --- Low-level references ---
	GetRef(name string) (string, error)
	UpdateRef(name, sha string) error
	UpdateRefWithLog(ctx context.Context, refName, sha, message string) error
	UpdateRefsBatch(ctx context.Context, updates []RefUpdate) error
	UpdateRefsBatchWithLog(ctx context.Context, updates []RefUpdate, reflogMessage string) error
	DeleteRefsBatch(ctx context.Context, refNames []string) error
	VerifyRef(ctx context.Context, refName string) error
	DeleteRef(ctx context.Context, name string) error
	ListRefs(prefix string) (map[string]string, error)

	// --- Low-level objects ---
	CreateBlob(content string) (string, error)
	// CreateBlobsBatch writes N blobs in a single `git hash-object` invocation.
	// Returns SHAs in input order. For small N (<3) callers should still use
	// CreateBlob — the temp-file staging required by the batch path only pays
	// off once per-blob subprocess overhead would dominate. ctx is honored for
	// the underlying git invocation so long-running batches can be canceled.
	CreateBlobsBatch(ctx context.Context, contents []string) ([]string, error)
	ReadBlob(sha string) (string, error)
	CatFile(sha string) (string, error)

	// --- Stackit branch metadata persistence ---
	ReadMetadata(branchName string) (*Meta, error)
	BatchReadMetadata(branchNames []string) (map[string]*Meta, map[string]error)
	WriteMetadata(branchName string, meta *Meta) error
	DeleteMetadata(ctx context.Context, branchName string) error
	RenameMetadata(oldName, newName string) error
	ListMetadata() (map[string]string, error)
	ReadLocalMetadata(branchName string) (*LocalMeta, error)
	BatchReadLocalMetadata(branchNames []string) map[string]*LocalMeta
	WriteLocalMetadata(branchName string, meta *LocalMeta) error
	// WriteMetadataBlobsBatch and WriteLocalMetadataBlobsBatch marshal each entry
	// and forward to CreateBlobsBatch — call them with len(metas) >= 1 from
	// engine_writer.go and transaction.go's commit path. ctx is honored for the
	// underlying git hash-object invocation.
	WriteMetadataBlobsBatch(ctx context.Context, metas []*Meta) ([]string, error)
	WriteLocalMetadataBlobsBatch(ctx context.Context, metas []*LocalMeta) ([]string, error)
	GetMetadataRefSHA(branchName string) string
	GetLocalMetadataRefSHA(branchName string) string
	ClearMetadataCache()
	// MetadataCacheStats returns cumulative cache hit/miss counts since process start.
	// Used by tests and instrumentation to verify lazy-load behavior.
	MetadataCacheStats() MetadataCacheSummary

	// --- Stack-level metadata persistence (separate from branch metadata; survives branch operations) ---
	ReadStackMeta(stackID string) (*StackMeta, error)
	WriteStackMeta(stackID string, meta *StackMeta) error
	DeleteStackMeta(ctx context.Context, stackID string) error
	ListStackMetas() (map[string]string, error)
	WriteStackMetaBlob(meta *StackMeta) (string, error)
	GetStackMetaRefSHA(stackID string) string

	// --- Raw command execution ---
	RunGitCommandWithContext(ctx context.Context, args ...string) (string, error)
	RunGitCommandRawWithContext(ctx context.Context, args ...string) (string, error)
	RunGitCommandWithEnv(ctx context.Context, env []string, args ...string) (string, error)
	RunGitCommandInteractive(args ...string) error
	RunGHCommandWithContext(ctx context.Context, args ...string) (string, error)

	// --- Logging ---
	SetLogger(logger DebugLogger)
}
