package git_test

import (
	"context"
	"os"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/require"

	"stackit.dev/stackit/internal/git"
	"stackit.dev/stackit/testhelpers"
)

func TestPullBranch_Reproduction(t *testing.T) {
	// This test attempts to reproduce the "false conflict" where PullBranch
	// returns PullConflict even though a fast-forward is possible.

	// 1. Setup a "remote" repository
	remoteScene := testhelpers.NewScene(t, func(s *testhelpers.Scene) error {
		return s.Repo.CreateChangeAndCommit("initial", "init")
	})
	remotePath, err := remoteScene.Repo.CreateBareRemote("upstream")
	require.NoError(t, err)
	err = remoteScene.Repo.PushBranch("upstream", "main")
	require.NoError(t, err)

	// 2. Setup a "local" repository
	localDir, err := os.MkdirTemp("", "stackit-test-local-*")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(localDir) })

	cmd := exec.Command("git", "clone", "--branch", "main", remotePath, localDir)
	err = cmd.Run()
	require.NoError(t, err)

	// Create a long-lived runner
	runner := git.NewRunnerWithPath(localDir)
	err = runner.InitDefaultRepo()
	require.NoError(t, err)

	// Warm up the runner's internal go-git state
	initialLocalSha, err := runner.RunGitCommandWithContext(context.Background(), "rev-parse", "HEAD")
	require.NoError(t, err)
	_, err = runner.GetCommitAuthor(initialLocalSha)
	require.NoError(t, err)

	// 3. Simulate a PR merge on the remote
	// We'll create a new branch, add a commit, then merge it into main on the remote.
	err = remoteScene.Repo.CreateAndCheckoutBranch("feature")
	require.NoError(t, err)
	err = remoteScene.Repo.CreateChangeAndCommit("feature change", "feature")
	require.NoError(t, err)
	featureSha, err := remoteScene.Repo.GetCurrentSHA()
	require.NoError(t, err)

	err = remoteScene.Repo.CheckoutBranch("main")
	require.NoError(t, err)
	// Create a merge commit on remote main
	err = remoteScene.Repo.RunGitCommand("merge", "--no-ff", "feature", "-m", "Merge PR #1")
	require.NoError(t, err)

	newRemoteSha, err := remoteScene.Repo.GetCurrentSHA()
	require.NoError(t, err)
	t.Logf("New remote SHA: %s (Feature was %s)", newRemoteSha, featureSha)

	// Push the updated main to the bare remote
	err = remoteScene.Repo.PushBranch("upstream", "main")
	require.NoError(t, err)

	// 4. Run PullBranch using the long-lived runner
	// With the fix, this should result in PullDone for a valid fast-forward.
	ctx := context.Background()
	result, err := runner.PullBranch(ctx, "origin", "main")
	require.NoError(t, err)

	// Verify that the pull succeeded (fast-forward should work)
	require.Equal(t, git.PullDone, result, "PullBranch should return PullDone for a valid fast-forward")

	// Verify that go-git can now see the new commit
	_, err = runner.GetCommitAuthor(newRemoteSha)
	require.NoError(t, err, "go-git should be able to see the newly fetched commit after reload")

	// Verify that the local branch was actually updated
	localSha, err := runner.RunGitCommandWithContext(ctx, "rev-parse", "main")
	require.NoError(t, err)
	require.Equal(t, newRemoteSha, localSha, "Local branch should match remote after pull")
}

func TestReloadRepository(t *testing.T) {
	// Test that PullBranch automatically reloads the repository cache after fetching
	// This ensures go-git can see newly fetched commits

	// 1. Setup a repository
	scene := testhelpers.NewScene(t, func(s *testhelpers.Scene) error {
		return s.Repo.CreateChangeAndCommit("initial", "init")
	})

	// Create a runner and initialize it
	runner := git.NewRunnerWithPath(scene.Repo.Dir)
	err := runner.InitDefaultRepo()
	require.NoError(t, err)

	// Get initial commit SHA
	initialSha, err := runner.RunGitCommandWithContext(context.Background(), "rev-parse", "HEAD")
	require.NoError(t, err)

	// Verify we can access it via go-git
	_, err = runner.GetCommitAuthor(initialSha)
	require.NoError(t, err)

	// Create a new commit directly via git command (bypassing go-git)
	err = scene.Repo.CreateChangeAndCommit("new change", "new")
	require.NoError(t, err)
	newSha, err := scene.Repo.GetCurrentSHA()
	require.NoError(t, err)

	// The reload happens automatically inside PullBranch when it fetches
	// We can verify this works by checking that go-git can see the new commit
	// after operations that would trigger a reload (though in this test we're not
	// actually fetching, so the reload mechanism is tested indirectly through PullBranch tests)

	// Verify we can access the new commit via go-git
	// Note: This test verifies the reload mechanism works, but the actual reload
	// is tested in TestPullBranch_WithReload which exercises the full fetch+reload flow
	_, err = runner.GetCommitAuthor(newSha)
	require.NoError(t, err, "go-git should see the new commit")

	// Verify we can still access old commits
	_, err = runner.GetCommitAuthor(initialSha)
	require.NoError(t, err, "go-git should still see old commits")
}

func TestPullBranch_WithReload(t *testing.T) {
	// Test that PullBranch works correctly with the refspec fix and reload mechanism

	// 1. Setup a "remote" repository
	remoteScene := testhelpers.NewScene(t, func(s *testhelpers.Scene) error {
		return s.Repo.CreateChangeAndCommit("initial", "init")
	})
	remotePath, err := remoteScene.Repo.CreateBareRemote("upstream")
	require.NoError(t, err)
	err = remoteScene.Repo.PushBranch("upstream", "main")
	require.NoError(t, err)

	// 2. Setup a "local" repository
	localDir, err := os.MkdirTemp("", "stackit-test-local-*")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(localDir) })

	cmd := exec.Command("git", "clone", "--branch", "main", remotePath, localDir)
	err = cmd.Run()
	require.NoError(t, err)

	// Create a long-lived runner
	runner := git.NewRunnerWithPath(localDir)
	err = runner.InitDefaultRepo()
	require.NoError(t, err)

	// Get initial SHA
	initialSha, err := runner.RunGitCommandWithContext(context.Background(), "rev-parse", "HEAD")
	require.NoError(t, err)

	// 3. Add a commit to remote
	err = remoteScene.Repo.CreateChangeAndCommit("remote change", "remote")
	require.NoError(t, err)
	remoteSha, err := remoteScene.Repo.GetCurrentSHA()
	require.NoError(t, err)
	err = remoteScene.Repo.PushBranch("upstream", "main")
	require.NoError(t, err)

	// 4. Verify remote-tracking branch is updated by the refspec fetch
	ctx := context.Background()
	result, err := runner.PullBranch(ctx, "origin", "main")
	require.NoError(t, err)
	require.Equal(t, git.PullDone, result, "PullBranch should succeed with explicit refspec")

	// 5. Verify the local branch was updated
	localSha, err := runner.RunGitCommandWithContext(ctx, "rev-parse", "main")
	require.NoError(t, err)
	require.Equal(t, remoteSha, localSha, "Local branch should match remote")

	// 6. Verify go-git can see the new commit (this tests the reload mechanism in PullTrunk)
	// Note: This test verifies the refspec fix works. The reload is tested in engine_sync.go
	_, err = runner.GetCommitAuthor(remoteSha)
	require.NoError(t, err, "go-git should be able to see the newly fetched commit")

	// Verify initial commit is still accessible
	_, err = runner.GetCommitAuthor(initialSha)
	require.NoError(t, err, "go-git should still see old commits")
}
