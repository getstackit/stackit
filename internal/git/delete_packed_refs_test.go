package git_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/testhelpers"
)

// Regression tests for the silent packed-refs deletion bug.
//
// The original implementation only deleted the loose ref file. If a ref lived
// in .git/packed-refs (e.g., because `git gc` or `git pack-refs` ran), the
// call returned nil but the ref survived. That caused merge ship's cleanup to
// silently leave a packed branch behind, recreate its metadata via post-merge
// sync, and then prompt on a phantom metadata conflict. The fix routes delete
// through `git update-ref -d`, which rewrites packed-refs correctly.
//
// These tests pack the relevant refs before calling our delete APIs and assert
// the refs are actually gone afterward.

func TestDeleteBranch_RemovesPackedRef(t *testing.T) {
	t.Parallel()
	scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)
	runner := git.NewRunnerWithPath(scene.Dir, nil)

	ctx := t.Context()

	require.NoError(t, scene.Repo.CreateBranch("packed-feature"))
	packAllRefs(t, scene.Dir)

	requireLooseRefAbsent(t, scene.Dir, "refs/heads/packed-feature")
	requirePackedRefPresent(t, scene.Dir, "refs/heads/packed-feature")

	require.NoError(t, runner.DeleteBranch(ctx, "packed-feature"))

	requireRefAbsent(t, runner, "refs/heads/packed-feature")
	requirePackedRefAbsent(t, scene.Dir, "refs/heads/packed-feature")
}

func TestDeleteBranch_NoErrorWhenBranchMissing(t *testing.T) {
	t.Parallel()
	scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)
	runner := git.NewRunnerWithPath(scene.Dir, nil)

	// `git update-ref -d` is lenient for missing refs; deleting a never-existed
	// branch should succeed so callers can treat "already gone" as a no-op
	// without branching on error types.
	require.NoError(t, runner.DeleteBranch(t.Context(), "nope"))
}

func TestDeleteBranch_RefusesCheckedOutWorktreeBranch(t *testing.T) {
	t.Parallel()
	scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)
	runner := git.NewRunnerWithPath(scene.Dir, nil)

	ctx := t.Context()

	require.NoError(t, scene.Repo.CreateBranch("feature"))
	worktreePath := filepath.Join(t.TempDir(), "feature-worktree")
	_, err := runner.RunGitCommandWithContext(ctx, "worktree", "add", worktreePath, "feature")
	require.NoError(t, err)

	err = runner.DeleteBranch(ctx, "feature")
	require.Error(t, err)
	require.Contains(t, err.Error(), "checked out in worktree")
	requireRefPresent(t, runner, "refs/heads/feature")
}

func TestDeleteRefs_RefusesCheckedOutWorktreeBranch(t *testing.T) {
	t.Parallel()
	scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)
	runner := git.NewRunnerWithPath(scene.Dir, nil)

	ctx := t.Context()

	require.NoError(t, scene.Repo.CreateBranch("feature"))
	worktreePath := filepath.Join(t.TempDir(), "feature-worktree")
	_, err := runner.RunGitCommandWithContext(ctx, "worktree", "add", worktreePath, "feature")
	require.NoError(t, err)

	err = runner.DeleteRefs(ctx, []string{"refs/heads/feature"}...)
	require.Error(t, err)
	require.Contains(t, err.Error(), "checked out in worktree")
	requireRefPresent(t, runner, "refs/heads/feature")
}

func TestDeleteRef_RemovesPackedRef(t *testing.T) {
	t.Parallel()
	scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)
	runner := git.NewRunnerWithPath(scene.Dir, nil)

	sha, err := git.One(runner.CreateBlobs(t.Context(), "payload"))
	require.NoError(t, err)
	require.NoError(t, runner.UpdateRefs(t.Context(), []git.RefUpdate{{RefName: "refs/stackit/metadata/packed-feature", NewSHA: sha}}, ""))
	packAllRefs(t, scene.Dir)

	requireLooseRefAbsent(t, scene.Dir, "refs/stackit/metadata/packed-feature")
	requirePackedRefPresent(t, scene.Dir, "refs/stackit/metadata/packed-feature")

	require.NoError(t, runner.DeleteRefs(t.Context(), "refs/stackit/metadata/packed-feature"))

	requireRefAbsent(t, runner, "refs/stackit/metadata/packed-feature")
	requirePackedRefAbsent(t, scene.Dir, "refs/stackit/metadata/packed-feature")
}

func TestDeleteMetadata_RemovesPackedRef(t *testing.T) {
	t.Parallel()
	scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)
	runner := git.NewRunnerWithPath(scene.Dir, nil)
	runnerMetadata := git.NewMetadataStore(runner)

	// Covers the merge-ship symptom directly: cleanup deletes branch metadata
	// via this entry point, which previously left packed metadata refs behind
	// to be resurrected by the next sync as a phantom conflict.
	sha, err := git.One(runner.CreateBlobs(t.Context(), `{"parentBranchName":"main"}`))
	require.NoError(t, err)
	require.NoError(t, runner.UpdateRefs(t.Context(), []git.RefUpdate{{RefName: "refs/stackit/metadata/packed-feature", NewSHA: sha}}, ""))
	packAllRefs(t, scene.Dir)

	require.NoError(t, runnerMetadata.DeleteMetadata(t.Context(), "packed-feature"))

	requireRefAbsent(t, runner, "refs/stackit/metadata/packed-feature")
	requirePackedRefAbsent(t, scene.Dir, "refs/stackit/metadata/packed-feature")
}

func packAllRefs(t *testing.T, repoDir string) {
	t.Helper()
	// `--all` packs all refs under refs/, including non-branch refs like
	// refs/stackit/metadata/*. `--prune` removes the loose files after packing
	// so the test reflects the same state seen in user repos that ran git gc.
	r := git.NewRunnerWithPath(repoDir, nil)
	out, err := r.RunGitCommandWithContext(t.Context(), "pack-refs", "--all", "--prune")
	require.NoError(t, err, "pack-refs failed: %s", out)
}

func requireRefAbsent(t *testing.T, runner git.Runner, refName string) {
	t.Helper()
	_, err := runner.ReadRevisions(t.Context(), refName).One()
	require.Error(t, err, "ref %s should not resolve", refName)
}

func requireRefPresent(t *testing.T, runner git.Runner, refName string) {
	t.Helper()
	_, err := runner.ReadRevisions(t.Context(), refName).One()
	require.NoError(t, err, "ref %s should still resolve", refName)
}

func requireLooseRefAbsent(t *testing.T, repoDir, refName string) {
	t.Helper()
	_, err := os.Stat(filepath.Join(repoDir, ".git", refName))
	require.ErrorIs(t, err, os.ErrNotExist, "expected no loose ref file for %s, got err=%v", refName, err)
}

func requirePackedRefPresent(t *testing.T, repoDir, refName string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoDir, ".git", "packed-refs"))
	require.NoError(t, err, "reading packed-refs")
	require.Contains(t, string(data), " "+refName+"\n",
		"expected packed-refs to contain %s; contents:\n%s", refName, data)
}

func requirePackedRefAbsent(t *testing.T, repoDir, refName string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoDir, ".git", "packed-refs"))
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	require.NoError(t, err)
	require.NotContains(t, string(data), " "+refName+"\n",
		"packed-refs still contains %s after delete; contents:\n%s", refName, data)
}
