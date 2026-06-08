package git_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/testhelpers"
)

func TestPushBranchWithExplicitForceWithLease(t *testing.T) {
	t.Parallel()

	remoteScene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)
	remotePath, err := remoteScene.Repo.CreateBareRemote("origin")
	require.NoError(t, err)
	require.NoError(t, remoteScene.Repo.PushBranch("origin", "main"))

	require.NoError(t, remoteScene.Repo.CreateAndCheckoutBranch("feature"))
	require.NoError(t, remoteScene.Repo.CreateChangeAndCommit("feature v1", "feature"))
	require.NoError(t, remoteScene.Repo.PushBranch("origin", "feature"))

	localDir := filepath.Join(t.TempDir(), "local")
	localRepo, err := testhelpers.NewGitRepoFromURL(localDir, remotePath)
	require.NoError(t, err)
	require.NoError(t, localRepo.RunGitCommand("checkout", "-b", "feature", "origin/feature"))

	require.NoError(t, remoteScene.Repo.CreateChangeAndCommit("feature v2", "feature"))
	require.NoError(t, remoteScene.Repo.ForcePushBranch("origin", "feature"))

	runner := git.NewRunnerWithPath(localDir, nil)
	remoteShas, err := runner.FetchRemoteShas(context.Background(), "origin")
	require.NoError(t, err)
	observedRemoteSHA := remoteShas["feature"]
	require.NotEmpty(t, observedRemoteSHA)

	trackingSHA, err := localRepo.RunGitCommandAndGetOutput("rev-parse", "origin/feature")
	require.NoError(t, err)
	require.NotEqual(t, observedRemoteSHA, trackingSHA)

	require.NoError(t, os.WriteFile(filepath.Join(localDir, "local.txt"), []byte("local rewrite\n"), 0600))
	require.NoError(t, localRepo.RunGitCommand("add", "local.txt"))
	require.NoError(t, localRepo.RunGitCommand("commit", "-m", "local rewrite"))

	err = runner.PushBranch(context.Background(), "feature", "origin", git.PushOptions{ForceWithLease: true})
	require.Error(t, err)

	err = runner.PushBranch(context.Background(), "feature", "origin", git.PushOptions{
		ForceWithLease:            true,
		ForceWithLeaseExpectedSHA: observedRemoteSHA,
	})
	require.NoError(t, err)

	localSHA, err := localRepo.RunGitCommandAndGetOutput("rev-parse", "feature")
	require.NoError(t, err)
	remoteAfterPush, err := runner.FetchRemoteShas(context.Background(), "origin")
	require.NoError(t, err)
	require.Equal(t, localSHA, remoteAfterPush["feature"])
}

func TestPushBranchesCreatesMultipleBranchesInOnePush(t *testing.T) {
	t.Parallel()

	scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)
	_, err := scene.Repo.CreateBareRemote("origin")
	require.NoError(t, err)
	require.NoError(t, scene.Repo.PushBranch("origin", "main"))

	require.NoError(t, scene.Repo.CreateAndCheckoutBranch("f1"))
	require.NoError(t, scene.Repo.CreateChangeAndCommit("f1 v1", "f1"))
	require.NoError(t, scene.Repo.CreateAndCheckoutBranch("f2"))
	require.NoError(t, scene.Repo.CreateChangeAndCommit("f2 v1", "f2"))

	runner := git.NewRunnerWithPath(scene.Dir, nil)
	// Empty ExpectedRemoteSHA means "expect absent" — the create case.
	results := runner.PushBranches(context.Background(), "origin", []git.PushSpec{
		{BranchName: "f1"},
		{BranchName: "f2"},
	}, git.PushOptions{})

	require.NoError(t, results["f1"])
	require.NoError(t, results["f2"])

	remote, err := runner.FetchRemoteShas(context.Background(), "origin")
	require.NoError(t, err)
	require.Contains(t, remote, "f1")
	require.Contains(t, remote, "f2")
}

func TestPushBranchesPartialSuccessOnStaleLease(t *testing.T) {
	t.Parallel()

	scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)
	remotePath, err := scene.Repo.CreateBareRemote("origin")
	require.NoError(t, err)
	require.NoError(t, scene.Repo.PushBranch("origin", "main"))

	// Two branches, both already on the remote.
	require.NoError(t, scene.Repo.CreateAndCheckoutBranch("f1"))
	require.NoError(t, scene.Repo.CreateChangeAndCommit("f1 v1", "f1"))
	require.NoError(t, scene.Repo.PushBranch("origin", "f1"))
	require.NoError(t, scene.Repo.CheckoutBranch("main"))
	require.NoError(t, scene.Repo.CreateAndCheckoutBranch("f2"))
	require.NoError(t, scene.Repo.CreateChangeAndCommit("f2 v1", "f2"))
	require.NoError(t, scene.Repo.PushBranch("origin", "f2"))

	runner := git.NewRunnerWithPath(scene.Dir, nil)
	before, err := runner.FetchRemoteShas(context.Background(), "origin")
	require.NoError(t, err)

	// Advance f1 on the remote out-of-band so the recorded lease goes stale.
	otherDir := filepath.Join(t.TempDir(), "other")
	otherRepo, err := testhelpers.NewGitRepoFromURL(otherDir, remotePath)
	require.NoError(t, err)
	require.NoError(t, otherRepo.RunGitCommand("checkout", "-b", "f1", "origin/f1"))
	require.NoError(t, otherRepo.CreateChangeAndCommit("f1 external", "f1ext"))
	require.NoError(t, otherRepo.PushBranch("origin", "f1"))

	// Advance both branches locally so each has something to push.
	require.NoError(t, scene.Repo.CheckoutBranch("f1"))
	require.NoError(t, scene.Repo.CreateChangeAndCommit("f1 v2", "f1local"))
	require.NoError(t, scene.Repo.CheckoutBranch("f2"))
	require.NoError(t, scene.Repo.CreateChangeAndCommit("f2 v2", "f2local"))

	results := runner.PushBranches(context.Background(), "origin", []git.PushSpec{
		{BranchName: "f1", ExpectedRemoteSHA: before["f1"]}, // stale: remote moved
		{BranchName: "f2", ExpectedRemoteSHA: before["f2"]}, // current
	}, git.PushOptions{})

	require.ErrorIs(t, results["f1"], git.ErrStaleRemoteInfo, "stale lease must surface ErrStaleRemoteInfo")
	require.NoError(t, results["f2"], "the in-sync branch must still push despite f1's rejection")

	// f2 advanced on the remote, f1 did not.
	after, err := runner.FetchRemoteShas(context.Background(), "origin")
	require.NoError(t, err)
	f2SHA, err := scene.Repo.RunGitCommandAndGetOutput("rev-parse", "f2")
	require.NoError(t, err)
	require.Equal(t, f2SHA, after["f2"])
}
