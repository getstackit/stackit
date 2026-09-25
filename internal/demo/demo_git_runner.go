package demo

import (
	"context"
	"time"

	"github.com/getstackit/stackit/internal/git"
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
		trunk:         demoTrunkBranch,
		currentBranch: demoTrunkBranch,
		branches:      demoBranches,
	}
}

func (d *demoGitRunner) InitDefaultRepo() error {
	return nil
}

func (d *demoGitRunner) GetRemote() string {
	return "origin"
}

// GitVersion reports a version new enough to clear every feature gate, since
// demo mode never shells out to git.
func (d *demoGitRunner) GitVersion(_ context.Context) (git.Version, error) {
	return git.MinimumGitVersion, nil
}

func (d *demoGitRunner) FetchRemoteShas(_ context.Context, _ string) (map[string]string, error) {
	return make(map[string]string), nil
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

func (d *demoGitRunner) ListIgnoredFiles(_ context.Context, _ string) ([]string, error) {
	return nil, nil
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

func (d *demoGitRunner) GetAllBranchNames(_ context.Context) ([]string, error) {
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

func (d *demoGitRunner) UpdateBranchRefCAS(_ context.Context, _, _, _ string) error {
	return nil
}

func (d *demoGitRunner) readRevision(branchName string) string {
	for _, b := range d.branches {
		if b.Name == branchName {
			return b.SHA
		}
	}
	return "rev-sha"
}

func (d *demoGitRunner) GetMergeBase(_ context.Context, _, _ string) (string, error) {
	return "merge-base-sha", nil
}

func (d *demoGitRunner) IsAncestor(_ context.Context, _, _ string) (bool, error) {
	return true, nil
}

func (d *demoGitRunner) ReadAncestry(_ context.Context, ranges ...git.RevRange) git.ReadResults[bool] {
	result := git.ReadResults[bool]{}
	for _, rr := range ranges {
		result.Set(rr.String(), true)
	}
	return result
}

func (d *demoGitRunner) ReadCommitCounts(_ context.Context, ranges ...git.RevRange) git.ReadResults[int] {
	result := git.ReadResults[int]{}
	for _, rr := range ranges {
		result.Set(rr.String(), 1)
	}
	return result
}

func (d *demoGitRunner) PullBranch(_ context.Context, _, _ string) (git.PullResult, error) {
	return git.PullDone, nil
}

func (d *demoGitRunner) UpdateBranchFromRemote(_ context.Context, _, _ string) (git.PullResult, error) {
	return git.PullDone, nil
}

func (d *demoGitRunner) PushBranches(_ context.Context, _ string, specs []git.PushSpec, _ git.PushOptions) git.PushResults {
	results := make(map[string]error, len(specs))
	for _, s := range specs {
		results[s.BranchName] = nil
	}
	return results
}

func (d *demoGitRunner) Rebase(_ context.Context, _, _, _ string) (git.RebaseOutcome, error) {
	return git.RebaseOutcome{Result: git.RebaseDone}, nil
}

func (d *demoGitRunner) RebaseContinue(_ context.Context) (git.RebaseOutcome, error) {
	return git.RebaseOutcome{Result: git.RebaseDone}, nil
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

func (d *demoGitRunner) StashDrop(_ context.Context, _ string) error {
	return nil
}

func (d *demoGitRunner) StashPop(_ context.Context) error {
	return nil
}

func (d *demoGitRunner) StashPopRef(_ context.Context, _ string) error {
	return nil
}

func (d *demoGitRunner) FetchRefs(_ context.Context, _ string, _ ...string) error {
	return nil
}

func (d *demoGitRunner) StashCreate(_ context.Context, _ string) (git.StashCapture, error) {
	return git.StashCapture{}, nil
}

func (d *demoGitRunner) RestoreIntentToAdd(_ context.Context, _ []string) error {
	return nil
}

func (d *demoGitRunner) StashApplyRef(_ context.Context, _ string, _ git.StashApplyMode) error {
	return nil
}

func (d *demoGitRunner) CaptureUntracked(_ context.Context, _ string) (string, error) {
	return "", nil
}

func (d *demoGitRunner) RestoreUntracked(_ context.Context, _ string) (int, error) {
	return 0, nil
}

func (d *demoGitRunner) Fetch(_ context.Context, _, _ string) error {
	return nil
}

func (d *demoGitRunner) FetchRefSpecs(_ context.Context, _ string, _ []string) error {
	return nil
}

func (d *demoGitRunner) CreateBranch(_ context.Context, _, _ string) error {
	return nil
}

func (d *demoGitRunner) CreateBranchForce(_ context.Context, branchName, _ string) error {
	d.branches = append(d.branches, Branch{Name: branchName})
	return nil
}

func (d *demoGitRunner) MergeBranches(_ context.Context, _ []string, _ git.MergeOptions) error {
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

func (d *demoGitRunner) StageChanges(_ context.Context, _ git.StagingOptions) error {
	return nil
}

func (d *demoGitRunner) GetRepoInfo(_ context.Context) (git.RemoteRepository, error) {
	return git.RemoteRepository{Host: "github.com", Owner: "owner", Name: "repo"}, nil
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

func (d *demoGitRunner) IsSquashMerged(_ context.Context, _, _ string, _ *git.SquashMergeCache) (bool, error) {
	return false, nil
}

func (d *demoGitRunner) GetMergedBranches(_ context.Context, _ string) (map[string]bool, error) {
	return make(map[string]bool), nil
}

func (d *demoGitRunner) ReadDiffs(_ context.Context, mode git.DiffReadMode, ranges ...git.RevRange) git.ReadResults[git.DiffSummary] {
	result := git.ReadResults[git.DiffSummary]{}
	for _, rr := range ranges {
		diff := git.DiffSummary{Files: []string{}}
		if mode == git.DiffStats {
			diff.Added, diff.Deleted = 1, 1
		}
		result.Set(rr.String(), diff)
	}
	return result
}

func (d *demoGitRunner) ShowDiff(_ context.Context, _, _ string, _ bool) (string, error) {
	return "diff", nil
}

func (d *demoGitRunner) ShowCommits(_ context.Context, _ git.RevRange, _, _ bool) (string, error) {
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

func (d *demoGitRunner) GetUnstagedDiffBinary(_ context.Context, _ ...string) (string, error) {
	return "", nil
}

func (d *demoGitRunner) GetDiffBetween(_ context.Context, _ git.RevRange, _ ...string) (string, error) {
	return "", nil
}

func (d *demoGitRunner) ReadCommitRanges(_ context.Context, ranges ...git.RevRange) git.ReadResults[[]git.CommitMetadata] {
	const message = "demo commit"
	const sha = "sha1"
	result := git.ReadResults[[]git.CommitMetadata]{}
	for _, rr := range ranges {
		result.Set(rr.String(), []git.CommitMetadata{{SHA: sha, ShortSHA: sha, Subject: message, Message: message}})
	}
	return result
}

func (d *demoGitRunner) BatchDiffNumstat(_ context.Context, ranges []git.RevRange) (map[git.RevRange]string, error) {
	result := make(map[git.RevRange]string, len(ranges))
	for _, rr := range ranges {
		result[rr] = "1\t1\ttest.txt"
	}
	return result, nil
}

func (d *demoGitRunner) ReadCommitNodes(ctx context.Context, ranges ...git.RevRange) git.ReadResults[[]git.CommitNode] {
	var result git.ReadResults[[]git.CommitNode]
	for key, commits := range d.ReadCommitRanges(ctx, ranges...).Values() {
		nodes := make([]git.CommitNode, len(commits))
		for i, commit := range commits {
			nodes[i] = git.CommitNode{SHA: commit.SHA, Parents: commit.Parents}
		}
		result.Set(key, nodes)
	}
	return result
}

func (d *demoGitRunner) ReadCommits(_ context.Context, refs ...string) git.ReadResults[git.CommitMetadata] {
	result := git.ReadResults[git.CommitMetadata]{}
	for _, ref := range refs {
		result.Set(ref, git.CommitMetadata{SHA: "sha1", Subject: "demo commit"})
	}
	return result
}

func (d *demoGitRunner) GetRecentCommits(_ context.Context, _ string, _ int) ([]git.RecentCommit, error) {
	return nil, nil
}

func (d *demoGitRunner) GetRecentCommitsInRange(_ context.Context, _ string) ([]git.RecentCommit, error) {
	return nil, nil
}

func (d *demoGitRunner) GetStatusPorcelain(_ context.Context) (string, error) {
	return "M  test.txt", nil
}

func (d *demoGitRunner) ReadWorktreeStatus(_ context.Context) (git.WorktreeStatus, error) {
	return git.WorktreeStatus{}, nil
}

func (d *demoGitRunner) GetCommitTemplate(_ context.Context) (string, error) {
	return "", nil
}

func (d *demoGitRunner) AddWorktree(_ context.Context, _, _ string, _ git.WorktreeDetachMode) error {
	return nil
}

func (d *demoGitRunner) AddWorktreeWithOptions(_ context.Context, _, _ string, _ git.WorktreeDetachMode, _ bool) error {
	return nil
}

func (d *demoGitRunner) RemoveWorktree(_ context.Context, _ string) error {
	return nil
}

func (d *demoGitRunner) ForceRemoveWorktree(_ context.Context, _ string) error {
	return nil
}

func (d *demoGitRunner) ListWorktrees(_ context.Context) (git.WorktreeList, error) {
	return git.WorktreeList{}, nil
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

func (d *demoGitRunner) WorktreeHasTrackedChanges(_ context.Context, _ string) (bool, error) {
	return false, nil
}

func (d *demoGitRunner) WorktreeResetBlocker(_ context.Context, _, _ string) string {
	return ""
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

func (d *demoGitRunner) ApplyPatchToWorktree(_ context.Context, _ string) error {
	return nil
}

func (d *demoGitRunner) CommitAmendNoEdit(_ context.Context) error {
	return nil
}

func (d *demoGitRunner) RebaseContinueNoEdit(_ context.Context) (git.RebaseOutcome, error) {
	return git.RebaseOutcome{Result: git.RebaseDone}, nil
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

func (d *demoGitRunner) ListRefs(_ string) (map[string]string, error) {
	return make(map[string]string), nil
}

func (d *demoGitRunner) RefDecorations() (map[string][]git.RefDecoration, error) {
	return make(map[string][]git.RefDecoration), nil
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

func (d *demoGitRunner) DeleteRemoteMetadataRefs(_ context.Context, _ ...string) error {
	return nil
}

func (d *demoGitRunner) TestRemoteRefCompatibility(_ context.Context) error {
	return nil
}

func (d *demoGitRunner) ReadWorktreeMeta(_ string) (*git.WorktreeMeta, error) {
	return nil, nil
}

func (d *demoGitRunner) WriteWorktreeMeta(_ context.Context, _ string, _ *git.WorktreeMeta) error {
	return nil
}

func (d *demoGitRunner) DeleteWorktreeMeta(_ context.Context, _ string) error {
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

// StackMetadataOperations methods

func (d *demoGitRunner) TreeContainsAnyPath(_ context.Context, _ string, _ []string) (bool, bool) {
	return false, true
}

func (d *demoGitRunner) GetUntrackedFilesIn(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}

func (d *demoGitRunner) ReadRevisions(_ context.Context, refs ...string) git.ReadResults[string] {
	result := git.ReadResults[string]{}
	for _, ref := range refs {
		result.Set(ref, d.readRevision(ref))
	}
	return result
}

func (d *demoGitRunner) ReadCommitInfo(_ context.Context, refs ...string) git.ReadResults[git.CommitInfo] {
	result := git.ReadResults[git.CommitInfo]{}
	for _, ref := range refs {
		result.Set(ref, git.CommitInfo{Date: time.Now(), Author: "Demo User"})
	}
	return result
}

func (d *demoGitRunner) CreateBlobs(_ context.Context, contents ...string) ([]string, error) {
	shas := make([]string, len(contents))
	for i := range contents {
		shas[i] = "blob-sha"
	}
	return shas, nil
}

func (d *demoGitRunner) UpdateRefs(_ context.Context, _ []git.RefUpdate, _ string) error { return nil }
func (d *demoGitRunner) DeleteRefs(_ context.Context, _ ...string) error                 { return nil }

func (d *demoGitRunner) RefGeneration(_ string) uint64 { return 0 }
func (d *demoGitRunner) ReadObjects(_ context.Context, _ ...string) (map[string]git.BatchObject, error) {
	return map[string]git.BatchObject{}, nil
}
