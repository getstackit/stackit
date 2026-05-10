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

	remoteScene := testhelpers.NewScene(t, testhelpers.InitialCommitSceneSetup)
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
