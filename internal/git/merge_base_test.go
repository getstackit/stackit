package git_test

import (
	"context"
	"os"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/testhelpers"
)

func TestIsAncestor(t *testing.T) {
	t.Run("returns true when commit is ancestor", func(t *testing.T) {
		scene := testhelpers.NewScene(t, testhelpers.InitialCommitSceneSetup)

		runner := git.NewRunnerWithPath(scene.Repo.Dir, nil)
		err := runner.InitDefaultRepo()
		require.NoError(t, err)

		// Get initial commit SHA
		initialSha, err := runner.RunGitCommandWithContext(context.Background(), "rev-parse", "HEAD")
		require.NoError(t, err)

		// Create another commit
		err = scene.Repo.CreateChangeAndCommit("second", "second")
		require.NoError(t, err)
		secondSha, err := scene.Repo.GetCurrentSHA()
		require.NoError(t, err)

		// Initial should be ancestor of second
		isAncestor, err := runner.IsAncestor(context.Background(), initialSha, secondSha)
		require.NoError(t, err)
		require.True(t, isAncestor, "initial commit should be ancestor of second commit")
	})

	t.Run("returns false when commit is not ancestor", func(t *testing.T) {
		scene := testhelpers.NewScene(t, testhelpers.InitialCommitSceneSetup)

		runner := git.NewRunnerWithPath(scene.Repo.Dir, nil)
		err := runner.InitDefaultRepo()
		require.NoError(t, err)

		// Get initial commit SHA
		initialSha, err := runner.RunGitCommandWithContext(context.Background(), "rev-parse", "HEAD")
		require.NoError(t, err)

		// Create another commit
		err = scene.Repo.CreateChangeAndCommit("second", "second")
		require.NoError(t, err)
		secondSha, err := scene.Repo.GetCurrentSHA()
		require.NoError(t, err)

		// Second should NOT be ancestor of initial
		isAncestor, err := runner.IsAncestor(context.Background(), secondSha, initialSha)
		require.NoError(t, err)
		require.False(t, isAncestor, "second commit should not be ancestor of initial commit")
	})

	t.Run("returns true when commits are the same", func(t *testing.T) {
		scene := testhelpers.NewScene(t, testhelpers.InitialCommitSceneSetup)

		runner := git.NewRunnerWithPath(scene.Repo.Dir, nil)
		err := runner.InitDefaultRepo()
		require.NoError(t, err)

		sha, err := runner.RunGitCommandWithContext(context.Background(), "rev-parse", "HEAD")
		require.NoError(t, err)

		// Same commit should be considered its own ancestor
		isAncestor, err := runner.IsAncestor(context.Background(), sha, sha)
		require.NoError(t, err)
		require.True(t, isAncestor, "commit should be its own ancestor")
	})
}

func TestIsAncestor_AfterFetch(t *testing.T) {
	// This test verifies the runner can resolve and walk to a newly fetched
	// commit. We test this by creating a scenario similar to a PR merge:
	// 1. Local repo has main at commit A
	// 2. Remote has main at commit B (which includes A as ancestor)
	// 3. We fetch but use a fresh runner that hasn't cached the commits
	// 4. IsAncestor should still resolve through `git merge-base --is-ancestor`

	t.Run("works after fetch with fresh runner", func(t *testing.T) {
		// 1. Setup a "remote" repository
		remoteScene := testhelpers.NewScene(t, testhelpers.InitialCommitSceneSetup)
		remotePath, err := remoteScene.Repo.CreateBareRemote("upstream")
		require.NoError(t, err)
		err = remoteScene.Repo.PushBranch("upstream", "main")
		require.NoError(t, err)

		// Get initial SHA from remote
		initialSha, err := remoteScene.Repo.GetCurrentSHA()
		require.NoError(t, err)

		// 2. Setup a "local" repository by cloning
		localDir, err := os.MkdirTemp("", "stackit-test-ancestor-*")
		require.NoError(t, err)
		t.Cleanup(func() { os.RemoveAll(localDir) })

		cmd := exec.Command("git", "clone", "--branch", "main", remotePath, localDir)
		err = cmd.Run()
		require.NoError(t, err)

		// 3. Add commits to remote (simulating PR merge)
		err = remoteScene.Repo.CreateChangeAndCommit("feature", "feature")
		require.NoError(t, err)
		newSha, err := remoteScene.Repo.GetCurrentSHA()
		require.NoError(t, err)
		err = remoteScene.Repo.PushBranch("upstream", "main")
		require.NoError(t, err)

		// 4. Create a runner for the local repo
		runner := git.NewRunnerWithPath(localDir, nil)
		err = runner.InitDefaultRepo()
		require.NoError(t, err)

		// 5. Fetch the new commits (but don't pull)
		_, err = runner.RunGitCommandWithContext(context.Background(),
			"fetch", "origin", "refs/heads/main:refs/remotes/origin/main")
		require.NoError(t, err)

		// 6. Test IsAncestor with the newly fetched commit
		isAncestor, err := runner.IsAncestor(context.Background(), initialSha, newSha)
		require.NoError(t, err)
		require.True(t, isAncestor, "initial should be ancestor of new commit after fetch")

		// Also verify the reverse is false
		isAncestor, err = runner.IsAncestor(context.Background(), newSha, initialSha)
		require.NoError(t, err)
		require.False(t, isAncestor, "new commit should not be ancestor of initial")
	})

	t.Run("works with diverged branches", func(t *testing.T) {
		// Test that IsAncestor correctly returns false for diverged branches
		scene := testhelpers.NewScene(t, testhelpers.InitialCommitSceneSetup)

		runner := git.NewRunnerWithPath(scene.Repo.Dir, nil)
		err := runner.InitDefaultRepo()
		require.NoError(t, err)

		// Get initial commit
		initialSha, err := runner.RunGitCommandWithContext(context.Background(), "rev-parse", "HEAD")
		require.NoError(t, err)

		// Create branch A with one commit
		err = scene.Repo.CreateAndCheckoutBranch("branch-a")
		require.NoError(t, err)
		err = scene.Repo.CreateChangeAndCommit("change-a", "a")
		require.NoError(t, err)
		shaA, err := scene.Repo.GetCurrentSHA()
		require.NoError(t, err)

		// Go back to initial and create branch B with different commit
		err = scene.Repo.CheckoutBranch("main")
		require.NoError(t, err)
		err = scene.Repo.CreateAndCheckoutBranch("branch-b")
		require.NoError(t, err)
		err = scene.Repo.CreateChangeAndCommit("change-b", "b")
		require.NoError(t, err)
		shaB, err := scene.Repo.GetCurrentSHA()
		require.NoError(t, err)

		// Neither should be ancestor of the other (diverged)
		isAncestor, err := runner.IsAncestor(context.Background(), shaA, shaB)
		require.NoError(t, err)
		require.False(t, isAncestor, "diverged commit A should not be ancestor of B")

		isAncestor, err = runner.IsAncestor(context.Background(), shaB, shaA)
		require.NoError(t, err)
		require.False(t, isAncestor, "diverged commit B should not be ancestor of A")

		// But initial should be ancestor of both
		isAncestor, err = runner.IsAncestor(context.Background(), initialSha, shaA)
		require.NoError(t, err)
		require.True(t, isAncestor, "initial should be ancestor of A")

		isAncestor, err = runner.IsAncestor(context.Background(), initialSha, shaB)
		require.NoError(t, err)
		require.True(t, isAncestor, "initial should be ancestor of B")
	})
}
