package demo

import (
	"context"
	"time"

	"stackit.dev/stackit/internal/git"
)

// demoGitRunner implements git.Runner with simulated data for demo mode.
type demoGitRunner struct {
	trunk         string
	currentBranch string
	branches      []Branch
}

// NewDemoGitRunner creates a new demo git runner with simulated data.
func NewDemoGitRunner() git.Runner {
	return &demoGitRunner{
		trunk:         "main",
		currentBranch: "main",
		branches:      demoBranches,
	}
}

func (d *demoGitRunner) InitDefaultRepo() error {
	return nil
}

func (d *demoGitRunner) GetRemote() string {
	return "origin"
}

func (d *demoGitRunner) FetchRemoteShas(_ context.Context, _ string) (map[string]string, error) {
	return make(map[string]string), nil
}

func (d *demoGitRunner) GetRemoteSha(_, _ string) (string, error) {
	return "remote-sha", nil
}

func (d *demoGitRunner) GetConfig(_ string) (string, error) {
	return "", nil
}

func (d *demoGitRunner) SetConfig(_, _ string) error {
	return nil
}

func (d *demoGitRunner) GetConfigAll(_ string) ([]string, error) {
	return []string{}, nil
}

func (d *demoGitRunner) AddConfigValue(_, _ string) error {
	return nil
}

func (d *demoGitRunner) GetUserName(_ context.Context) (string, error) {
	return "demo-user", nil
}

func (d *demoGitRunner) DiscoverRepoRoot() (string, error) {
	return "/demo/repo", nil
}

func (d *demoGitRunner) IsInsideRepo() bool {
	return true
}

func (d *demoGitRunner) GetRepoRoot() string {
	return "/demo/repo"
}

func (d *demoGitRunner) GetGitCommonDir() (string, error) {
	return "/demo/repo/.git", nil
}

func (d *demoGitRunner) EnsureMetadataRefspecConfigured() error {
	return nil
}

func (d *demoGitRunner) EnsureStackMetaRefspecConfigured() error {
	return nil
}

func (d *demoGitRunner) GetCurrentBranch() (string, error) {
	return d.currentBranch, nil
}

func (d *demoGitRunner) GetAllBranchNames() ([]string, error) {
	names := make([]string, len(d.branches))
	for i, b := range d.branches {
		names[i] = b.Name
	}
	return names, nil
}

func (d *demoGitRunner) FindRemoteBranch(_ context.Context, _ string) (string, error) {
	return "main", nil
}

func (d *demoGitRunner) CheckoutBranch(_ context.Context, branchName string) error {
	d.currentBranch = branchName
	return nil
}

func (d *demoGitRunner) CheckoutBranchForce(_ context.Context, branchName string) error {
	d.currentBranch = branchName
	return nil
}

func (d *demoGitRunner) CreateAndCheckoutBranch(_ context.Context, branchName string) error {
	d.branches = append(d.branches, Branch{Name: branchName})
	d.currentBranch = branchName
	return nil
}

func (d *demoGitRunner) DeleteBranch(_ context.Context, branchName string) error {
	for i, b := range d.branches {
		if b.Name == branchName {
			d.branches = append(d.branches[:i], d.branches[i+1:]...)
			break
		}
	}
	return nil
}

func (d *demoGitRunner) RenameBranch(_ context.Context, oldName, newName string) error {
	for i, b := range d.branches {
		if b.Name == oldName {
			d.branches[i].Name = newName
			break
		}
	}
	if d.currentBranch == oldName {
		d.currentBranch = newName
	}
	return nil
}

func (d *demoGitRunner) CheckoutDetached(_ context.Context, _ string) error {
	d.currentBranch = ""
	return nil
}

func (d *demoGitRunner) UpdateBranchRef(_ context.Context, _, _ string) error {
	return nil
}

func (d *demoGitRunner) GetRemoteRevision(_ string) (string, error) {
	return "remote-rev", nil
}

func (d *demoGitRunner) GetCurrentRevision(_ context.Context) (string, error) {
	return "head-sha", nil
}

func (d *demoGitRunner) GetRevision(branchName string) (string, error) {
	for _, b := range d.branches {
		if b.Name == branchName {
			return b.SHA, nil
		}
	}
	return "rev-sha", nil
}

func (d *demoGitRunner) BatchGetRevisions(branchNames []string) (map[string]string, []error) {
	results := make(map[string]string)
	for _, name := range branchNames {
		results[name], _ = d.GetRevision(name)
	}
	return results, nil
}

func (d *demoGitRunner) GetMergeBase(_, _ string) (string, error) {
	return "merge-base-sha", nil
}

func (d *demoGitRunner) GetMergeBaseByRef(_, _ string) (string, error) {
	return "merge-base-sha", nil
}

func (d *demoGitRunner) IsAncestor(_, _ string) (bool, error) {
	return true, nil
}

func (d *demoGitRunner) GetCommitDate(_ string) (time.Time, error) {
	return time.Now(), nil
}

func (d *demoGitRunner) GetCommitAuthor(_ string) (string, error) {
	return "Demo User", nil
}

func (d *demoGitRunner) GetCommitRange(_, _, _ string) ([]string, error) {
	return []string{"commit message"}, nil
}

func (d *demoGitRunner) GetCommitSHA(_ string, _ int) (string, error) {
	return "commit-sha", nil
}

func (d *demoGitRunner) PullBranch(_ context.Context, _, _ string) (git.PullResult, error) {
	return git.PullDone, nil
}

func (d *demoGitRunner) PushBranch(_ context.Context, _, _ string, _ git.PushOptions) error {
	return nil
}

func (d *demoGitRunner) Rebase(_ context.Context, _, _, _ string) (git.RebaseResult, error) {
	return git.RebaseDone, nil
}

func (d *demoGitRunner) RebaseContinue(_ context.Context) (git.RebaseResult, error) {
	return git.RebaseDone, nil
}

func (d *demoGitRunner) RebaseAbort(_ context.Context) error {
	return nil
}

func (d *demoGitRunner) InteractiveRebase(_ context.Context, _ string) error {
	return nil
}

func (d *demoGitRunner) IsMergeInProgress(_ context.Context) bool {
	return false
}

func (d *demoGitRunner) MergeAbort(_ context.Context) error {
	return nil
}

func (d *demoGitRunner) CherryPick(_ context.Context, commitSHA, _ string) (string, error) {
	return commitSHA, nil
}

func (d *demoGitRunner) StashPush(_ context.Context, _ string) (string, error) {
	return "stashed", nil
}

func (d *demoGitRunner) StashPushStaged(_ context.Context, _ string) (string, error) {
	return "stashed staged", nil
}

func (d *demoGitRunner) StashPop(_ context.Context) error {
	return nil
}

func (d *demoGitRunner) Fetch(_ context.Context, _, _ string) error {
	return nil
}

func (d *demoGitRunner) CreateBranch(_ context.Context, _, _ string) error {
	return nil
}

func (d *demoGitRunner) CreateBranchForce(_ context.Context, branchName, _ string) error {
	d.branches = append(d.branches, Branch{Name: branchName})
	return nil
}

func (d *demoGitRunner) Merge(_ context.Context, _ string, _ git.MergeOptions) error {
	return nil
}

func (d *demoGitRunner) MergeMultiple(_ context.Context, _ []string, _ git.MergeOptions) error {
	return nil
}

func (d *demoGitRunner) CheckoutPaths(_ context.Context, _ string, _ []string) error {
	return nil
}

func (d *demoGitRunner) RemovePaths(_ context.Context, _ []string) error {
	return nil
}

func (d *demoGitRunner) ResetMerge(_ context.Context, _ string) error {
	return nil
}

func (d *demoGitRunner) MixedReset(_ context.Context, _ string) error {
	return nil
}

func (d *demoGitRunner) ListStash(_ context.Context) (string, error) {
	return "", nil
}

func (d *demoGitRunner) GetReflog(_ context.Context, _ int, _ string) (string, error) {
	return "", nil
}

func (d *demoGitRunner) GetCommitRangeSHAs(_, _ string) ([]string, error) {
	return []string{"sha1", "sha2"}, nil
}

func (d *demoGitRunner) GetCommitHistorySHAs(_ string) ([]string, error) {
	return []string{"sha1", "sha2"}, nil
}

func (d *demoGitRunner) GetRebaseHead() (string, error) {
	return "rebase-head-sha", nil
}

func (d *demoGitRunner) IsRebaseInProgress(_ context.Context) bool {
	return false
}

func (d *demoGitRunner) CheckRebaseInProgress(_ context.Context) error {
	return nil
}

func (d *demoGitRunner) HasUncommittedChanges(_ context.Context) bool {
	return false
}

func (d *demoGitRunner) ParseStagedHunks(_ context.Context) ([]git.Hunk, error) {
	return []git.Hunk{}, nil
}

func (d *demoGitRunner) HardReset(_ context.Context, _ string) error {
	return nil
}

func (d *demoGitRunner) SoftReset(_ context.Context, _ string) error {
	return nil
}

func (d *demoGitRunner) CommitWithOptions(_ git.CommitOptions) error {
	return nil
}

func (d *demoGitRunner) Commit(_ string, _ int, _ bool) error {
	return nil
}

func (d *demoGitRunner) StageAll(_ context.Context) error {
	return nil
}

func (d *demoGitRunner) StageTracked(_ context.Context) error {
	return nil
}

func (d *demoGitRunner) HasUntrackedFiles(_ context.Context) (bool, error) {
	return false, nil
}

func (d *demoGitRunner) GetUntrackedFiles(_ context.Context) ([]string, error) {
	return nil, nil
}

func (d *demoGitRunner) AddAll(_ context.Context) error {
	return nil
}

func (d *demoGitRunner) StageChanges(_ context.Context, _ git.StagingOptions) error {
	return nil
}

func (d *demoGitRunner) GetRepoInfo(_ context.Context) (string, string, error) {
	return "owner", "repo", nil
}

func (d *demoGitRunner) HasStagedChanges(_ context.Context) (bool, error) {
	return false, nil
}

func (d *demoGitRunner) HasUnstagedChanges(_ context.Context) (bool, error) {
	return false, nil
}

func (d *demoGitRunner) IsMerged(_ context.Context, _, _ string) (bool, error) {
	return false, nil
}

func (d *demoGitRunner) GetMergedBranches(_ context.Context, _ string) (map[string]bool, error) {
	return make(map[string]bool), nil
}

func (d *demoGitRunner) IsDiffEmpty(_ context.Context, _, _ string) (bool, error) {
	return false, nil
}

func (d *demoGitRunner) GetChangedFiles(_ context.Context, _, _ string) ([]string, error) {
	return []string{}, nil
}

func (d *demoGitRunner) ShowDiff(_ context.Context, _, _ string, _ bool) (string, error) {
	return "diff", nil
}

func (d *demoGitRunner) ShowCommits(_ context.Context, _, _ string, _, _ bool) (string, error) {
	return "commits", nil
}

func (d *demoGitRunner) GetUnmergedFiles(_ context.Context) ([]string, error) {
	return []string{}, nil
}

func (d *demoGitRunner) GetStagedDiff(_ context.Context, _ ...string) (string, error) {
	return "", nil
}

func (d *demoGitRunner) GetUnstagedDiff(_ context.Context, _ ...string) (string, error) {
	return "", nil
}

func (d *demoGitRunner) GetDiffBetween(_ context.Context, _, _ string, _ ...string) (string, error) {
	return "", nil
}

func (d *demoGitRunner) GetDiffNumstat(_, _ string) (string, error) {
	return "1\t1\ttest.txt", nil
}

func (d *demoGitRunner) GetCommitLog(_, _ string) (string, error) {
	return "demo commit", nil
}

func (d *demoGitRunner) GetStatusPorcelain(_ context.Context) (string, error) {
	return "M  test.txt", nil
}

func (d *demoGitRunner) GetCommitTemplate(_ context.Context) (string, error) {
	return "", nil
}

func (d *demoGitRunner) AddWorktree(_ context.Context, _, _ string, _ bool) error {
	return nil
}

func (d *demoGitRunner) AddWorktreeWithOptions(_ context.Context, _, _ string, _, _ bool) error {
	return nil
}

func (d *demoGitRunner) RemoveWorktree(_ context.Context, _ string) error {
	return nil
}

func (d *demoGitRunner) ListWorktrees(_ context.Context) ([]string, error) {
	return []string{}, nil
}

func (d *demoGitRunner) PruneWorktrees(_ context.Context) error {
	return nil
}

func (d *demoGitRunner) GetWorktreePathForBranch(_ context.Context, _ string) (string, error) {
	return "", nil
}

func (d *demoGitRunner) GetWorktreeCurrentBranch(_ context.Context, _ string) (string, error) {
	return "", nil
}

func (d *demoGitRunner) ResetWorktreeWorkingDir(_ context.Context, _ string) error {
	return nil
}

func (d *demoGitRunner) WorktreeHasUncommittedChanges(_ context.Context, _ string) (bool, error) {
	return false, nil
}

func (d *demoGitRunner) UpdateRefWithLog(_ context.Context, _, _, _ string) error {
	return nil
}

func (d *demoGitRunner) VerifyRef(_ context.Context, _ string) error {
	return nil
}

func (d *demoGitRunner) GetCurrentBranchOrSHA(_ context.Context) (string, error) {
	return "demo-branch", nil
}

func (d *demoGitRunner) CherryPickSimple(_ context.Context, _ string) error {
	return nil
}

func (d *demoGitRunner) CherryPickAbort(_ context.Context) error {
	return nil
}

func (d *demoGitRunner) ApplyPatch(_ context.Context, _ string, _ bool) error {
	return nil
}

func (d *demoGitRunner) CommitAmendNoEdit(_ context.Context) error {
	return nil
}

func (d *demoGitRunner) RebaseContinueNoEdit(_ context.Context) (git.RebaseResult, error) {
	return git.RebaseDone, nil
}

func (d *demoGitRunner) StagePatch(_ context.Context) error {
	return nil
}

func (d *demoGitRunner) RunGitCommandWithEnv(_ context.Context, _ []string, _ ...string) (string, error) {
	return "", nil
}

func (d *demoGitRunner) RunGHCommandWithContext(_ context.Context, _ ...string) (string, error) {
	return "", nil
}

func (d *demoGitRunner) RunGitCommandWithContext(_ context.Context, _ ...string) (string, error) {
	return "", nil
}

func (d *demoGitRunner) RunGitCommandRawWithContext(_ context.Context, _ ...string) (string, error) {
	return "", nil
}

func (d *demoGitRunner) RunGitCommandInteractive(_ ...string) error {
	return nil
}

func (d *demoGitRunner) GetRef(_ string) (string, error) {
	return "ref-sha", nil
}

func (d *demoGitRunner) UpdateRef(_, _ string) error {
	return nil
}

func (d *demoGitRunner) DeleteRef(_ string) error {
	return nil
}

func (d *demoGitRunner) UpdateRefsBatch(_ context.Context, _ []git.RefUpdate) error {
	return nil
}

func (d *demoGitRunner) UpdateRefsBatchWithLog(_ context.Context, _ []git.RefUpdate, _ string) error {
	return nil
}

func (d *demoGitRunner) DeleteRefsBatch(_ context.Context, _ []string) error {
	return nil
}

func (d *demoGitRunner) CatFile(_ string) (string, error) {
	return "{}", nil
}

const demoBlobSHA = "blob-sha"
const demoRefSHA = "demo-ref-sha"

func (d *demoGitRunner) CreateBlob(_ string) (string, error) {
	return demoBlobSHA, nil
}

func (d *demoGitRunner) ReadBlob(_ string) (string, error) {
	return "{}", nil
}

func (d *demoGitRunner) ListRefs(_ string) (map[string]string, error) {
	return make(map[string]string), nil
}

func (d *demoGitRunner) ReadMetadata(_ string) (*git.Meta, error) {
	return &git.Meta{}, nil
}

func (d *demoGitRunner) WriteMetadata(_ string, _ *git.Meta) error {
	return nil
}

func (d *demoGitRunner) DeleteMetadata(_ string) error {
	return nil
}

func (d *demoGitRunner) RenameMetadata(_, _ string) error {
	return nil
}

func (d *demoGitRunner) ReadLocalMetadata(_ string) (*git.LocalMeta, error) {
	return &git.LocalMeta{}, nil
}

func (d *demoGitRunner) WriteLocalMetadata(_ string, _ *git.LocalMeta) error {
	return nil
}

func (d *demoGitRunner) ListMetadata() (map[string]string, error) {
	return make(map[string]string), nil
}

func (d *demoGitRunner) BatchReadMetadata(_ []string) (map[string]*git.Meta, map[string]error) {
	return make(map[string]*git.Meta), make(map[string]error)
}

func (d *demoGitRunner) BatchReadLocalMetadata(_ []string) map[string]*git.LocalMeta {
	return make(map[string]*git.LocalMeta)
}

func (d *demoGitRunner) GetParentCommitSHA(_ string) (string, error) {
	return "parent-sha", nil
}

func (d *demoGitRunner) CheckCommutation(_ git.Hunk, _, _ string) (bool, error) {
	return true, nil
}

func (d *demoGitRunner) PushMetadataRefs(_ context.Context, _ []string) error {
	return nil
}

func (d *demoGitRunner) FetchMetadataRefs(_ context.Context) error {
	return nil
}

func (d *demoGitRunner) PushStackMetaRefs(_ context.Context, _ []string) error {
	return nil
}

func (d *demoGitRunner) FetchStackMetaRefs(_ context.Context) error {
	return nil
}

func (d *demoGitRunner) DeleteRemoteStackMetaRefs(_ context.Context, _ []string) error {
	return nil
}

func (d *demoGitRunner) DeleteRemoteMetadataRef(_ context.Context, _ string) error {
	return nil
}

func (d *demoGitRunner) BatchDeleteRemoteMetadataRefs(_ context.Context, _ []string) error {
	return nil
}

func (d *demoGitRunner) TestRemoteRefCompatibility() error {
	return nil
}

func (d *demoGitRunner) ReadWorktreeMeta(_ string) (*git.WorktreeMeta, error) {
	return nil, nil
}

func (d *demoGitRunner) WriteWorktreeMeta(_ string, _ *git.WorktreeMeta) error {
	return nil
}

func (d *demoGitRunner) DeleteWorktreeMeta(_ string) error {
	return nil
}

func (d *demoGitRunner) ListWorktreeMetas() (map[string]*git.WorktreeMeta, error) {
	return make(map[string]*git.WorktreeMeta), nil
}

func (d *demoGitRunner) SetLogger(_ git.DebugLogger) {
	// No-op for demo runner
}

func (d *demoGitRunner) StageHunks(_ context.Context, _ []git.Hunk) error {
	return nil
}

func (d *demoGitRunner) UnstageAll(_ context.Context) error {
	return nil
}

func (d *demoGitRunner) WriteMetadataBlob(_ *git.Meta) (string, error) {
	return demoBlobSHA, nil
}

func (d *demoGitRunner) WriteLocalMetadataBlob(_ *git.LocalMeta) (string, error) {
	return demoBlobSHA, nil
}

func (d *demoGitRunner) GetMetadataRefSHA(_ string) string {
	// Return a mock SHA to simulate existing metadata refs for CAS testing
	return demoRefSHA
}

func (d *demoGitRunner) GetLocalMetadataRefSHA(_ string) string {
	// Return a mock SHA to simulate existing local metadata refs for CAS testing
	return demoRefSHA
}

// StackMetadataOperations methods

func (d *demoGitRunner) ReadStackMeta(_ string) (*git.StackMeta, error) {
	return nil, nil
}

func (d *demoGitRunner) WriteStackMeta(_ string, _ *git.StackMeta) error {
	return nil
}

func (d *demoGitRunner) DeleteStackMeta(_ string) error {
	return nil
}

func (d *demoGitRunner) ListStackMetas() (map[string]string, error) {
	return make(map[string]string), nil
}

func (d *demoGitRunner) WriteStackMetaBlob(_ *git.StackMeta) (string, error) {
	return demoBlobSHA, nil
}

func (d *demoGitRunner) GetStackMetaRefSHA(_ string) string {
	return ""
}
