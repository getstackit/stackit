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
	GetRepoInfo(ctx context.Context) (string, string, error)
}

// RepositoryWriter provides write access to repository configuration.
type RepositoryWriter interface {
	InitDefaultRepo() error
	SetConfig(key, value string) error
	AddConfigValue(key, value string) error
	EnsureMetadataRefspecConfigured() error
}

// RemoteOperations handles interaction with remote repositories.
type RemoteOperations interface {
	FetchRemoteShas(remote string) (map[string]string, error)
	GetRemoteSha(remote, branchName string) (string, error)
	GetRemoteRevision(branchName string) (string, error)
	FindRemoteBranch(ctx context.Context, remote string) (string, error)
	PushBranch(ctx context.Context, branchName, remote string, opts PushOptions) error
	PullBranch(ctx context.Context, remote, branchName string) (PullResult, error)
	Fetch(ctx context.Context, remote, branch string) error
	PushMetadataRefs(branches []string) error
	FetchMetadataRefs() error
	DeleteRemoteMetadataRef(branch string) error
	BatchDeleteRemoteMetadataRefs(branches []string) error
	TestRemoteRefCompatibility() error
}

// BranchReader provides read access to branch information.
type BranchReader interface {
	GetCurrentBranch() (string, error)
	GetAllBranchNames() ([]string, error)
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
}

// CommitReader provides read access to commit and revision information.
type CommitReader interface {
	GetRevision(branchName string) (string, error)
	GetCurrentRevision(ctx context.Context) (string, error)
	BatchGetRevisions(branchNames []string) (map[string]string, []error)
	GetCommitDate(branchName string) (time.Time, error)
	GetCommitAuthor(branchName string) (string, error)
	GetCommitRange(base, head, format string) ([]string, error)
	GetCommitRangeSHAs(base, head string) ([]string, error)
	GetCommitHistorySHAs(branchName string) ([]string, error)
	GetCommitSHA(branchName string, offset int) (string, error)
	GetCommitLog(sha, format string) (string, error)
	GetCommitTemplate(ctx context.Context) (string, error)
	GetParentCommitSHA(commitSHA string) (string, error)
}

// DiffOperations provides access to diff and comparison operations.
type DiffOperations interface {
	GetMergeBase(rev1, rev2 string) (string, error)
	GetMergeBaseByRef(ref1, ref2 string) (string, error)
	IsAncestor(ancestor, descendant string) (bool, error)
	IsMerged(ctx context.Context, branchName, target string) (bool, error)
	GetMergedBranches(ctx context.Context, target string) (map[string]bool, error)
	IsDiffEmpty(ctx context.Context, branchName, base string) (bool, error)
	GetChangedFiles(ctx context.Context, base, head string) ([]string, error)
	ShowDiff(ctx context.Context, left, right string, stat bool) (string, error)
	ShowCommits(ctx context.Context, base, head string, patch, stat bool) (string, error)
	GetDiffNumstat(base, head string) (string, error)
	GetStagedDiff(ctx context.Context, files ...string) (string, error)
	GetUnstagedDiff(ctx context.Context, files ...string) (string, error)
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
	ParseStagedHunks(ctx context.Context) ([]Hunk, error)
}

// CommitWriter handles commit creation and modification.
type CommitWriter interface {
	Commit(message string, verbose int, noVerify bool) error
	CommitWithOptions(opts CommitOptions) error
	CommitAmendNoEdit(ctx context.Context) error
}

// RebaseOperations handles rebase operations.
type RebaseOperations interface {
	Rebase(ctx context.Context, branchName, upstream, oldUpstream string) (RebaseResult, error)
	RebaseContinue(ctx context.Context) (RebaseResult, error)
	RebaseContinueNoEdit(ctx context.Context) (RebaseResult, error)
	RebaseAbort(ctx context.Context) error
	InteractiveRebase(ctx context.Context, onto string) error
	IsRebaseInProgress(ctx context.Context) bool
	GetRebaseHead() (string, error)
	CheckRebaseInProgress(ctx context.Context) error
}

// MergeOperations handles merge operations.
type MergeOperations interface {
	Merge(ctx context.Context, branchName string, opts MergeOptions) error
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
	StashPop(ctx context.Context) error
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
	CheckCommutation(hunk Hunk, commitSHA, parentSHA string) (bool, error)
}

// WorktreeOperations handles worktree management.
type WorktreeOperations interface {
	AddWorktree(ctx context.Context, path string, branch string, detach bool) error
	RemoveWorktree(ctx context.Context, path string) error
	ListWorktrees(ctx context.Context) ([]string, error)
	GetWorktreePathForBranch(ctx context.Context, branchName string) (string, error)
	GetWorktreeCurrentBranch(ctx context.Context, worktreePath string) (string, error)
	ResetWorktreeWorkingDir(ctx context.Context, worktreePath string) error
}

// WorktreeRegistryOperations handles stackit-managed worktree tracking (local-only refs).
type WorktreeRegistryOperations interface {
	ReadWorktreeMeta(stackRoot string) (*WorktreeMeta, error)
	WriteWorktreeMeta(stackRoot string, meta *WorktreeMeta) error
	DeleteWorktreeMeta(stackRoot string) error
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
	VerifyRef(ctx context.Context, refName string) error
	DeleteRef(name string) error
	ListRefs(prefix string) (map[string]string, error)
}

// ObjectOperations provides low-level Git object operations.
type ObjectOperations interface {
	CreateBlob(content string) (string, error)
	ReadBlob(sha string) (string, error)
	CatFile(sha string) (string, error)
}

// MetadataOperations handles stackit metadata persistence.
type MetadataOperations interface {
	ReadMetadata(branchName string) (*Meta, error)
	BatchReadMetadata(branchNames []string) (map[string]*Meta, map[string]error)
	WriteMetadata(branchName string, meta *Meta) error
	DeleteMetadata(branchName string) error
	RenameMetadata(oldName, newName string) error
	ListMetadata() (map[string]string, error)
	ReadLocalMetadata(branchName string) (*LocalMeta, error)
	BatchReadLocalMetadata(branchNames []string) map[string]*LocalMeta
	WriteLocalMetadata(branchName string, meta *LocalMeta) error
}
