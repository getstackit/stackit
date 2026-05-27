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

func TestPullBranch_Reproduction(t *testing.T) {
	// This test attempts to reproduce the "false conflict" where PullBranch
	// returns PullConflict even though a fast-forward is possible.

	// 1. Setup a "remote" repository
	remoteScene := testhelpers.NewScene(t, testhelpers.InitialCommitSceneSetup)
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
	runner := git.NewRunnerWithPath(localDir, nil)
	err = runner.InitDefaultRepo()
	require.NoError(t, err)

	// Warm up the runner's revision cache
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

	// Verify the cached revision is updated to the newly fetched commit
	_, err = runner.GetCommitAuthor(newRemoteSha)
	require.NoError(t, err, "runner should resolve the newly fetched commit after reload")

	// Verify that the local branch was actually updated
	localSha, err := runner.RunGitCommandWithContext(ctx, "rev-parse", "main")
	require.NoError(t, err)
	require.Equal(t, newRemoteSha, localSha, "Local branch should match remote after pull")
}

func TestReloadRepository(t *testing.T) {
	// Test that the runner can resolve commits created externally — the
	// revision cache must invalidate / pass through to git for unknown SHAs.

	// 1. Setup a repository
	scene := testhelpers.NewScene(t, testhelpers.InitialCommitSceneSetup)

	// Create a runner and initialize it
	runner := git.NewRunnerWithPath(scene.Repo.Dir, nil)
	err := runner.InitDefaultRepo()
	require.NoError(t, err)

	// Get initial commit SHA
	initialSha, err := runner.RunGitCommandWithContext(context.Background(), "rev-parse", "HEAD")
	require.NoError(t, err)

	// Verify we can resolve the initial commit
	_, err = runner.GetCommitAuthor(initialSha)
	require.NoError(t, err)

	// Create a new commit directly via git command (outside the runner's cache)
	err = scene.Repo.CreateChangeAndCommit("new change", "new")
	require.NoError(t, err)
	newSha, err := scene.Repo.GetCurrentSHA()
	require.NoError(t, err)

	// PullBranch normally triggers the reload after fetch; this test verifies
	// the runner falls through to git for SHAs that aren't in its cache. The
	// full fetch+reload flow is exercised in TestPullBranch_WithReload below.

	// Verify the new commit is resolvable
	_, err = runner.GetCommitAuthor(newSha)
	require.NoError(t, err, "runner should resolve the new commit")

	// Verify the initial commit is still resolvable
	_, err = runner.GetCommitAuthor(initialSha)
	require.NoError(t, err, "runner should still resolve old commits")
}

func TestPullBranch_WithReload(t *testing.T) {
	// Test that PullBranch works correctly with the refspec fix and reload mechanism

	// 1. Setup a "remote" repository
	remoteScene := testhelpers.NewScene(t, testhelpers.InitialCommitSceneSetup)
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
	runner := git.NewRunnerWithPath(localDir, nil)
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

	// 6. Verify the newly fetched commit is resolvable through the runner
	// (this exercises the reload mechanism in PullBranch).
	_, err = runner.GetCommitAuthor(remoteSha)
	require.NoError(t, err, "runner should resolve the newly fetched commit")

	// Verify initial commit is still accessible
	_, err = runner.GetCommitAuthor(initialSha)
	require.NoError(t, err, "runner should still resolve old commits")
}

func TestPullBranch_WorktreeCorruptsMainWorkspace(t *testing.T) {
	// This test reproduces a critical bug where pulling trunk in a worktree
	// corrupts the main workspace's index/working tree.
	//
	// Scenario (matching the real bug report):
	// 1. Main workspace is on "main" with a CLEAN working tree (no local changes)
	// 2. A PR is merged on GitHub, updating remote main
	// 3. A temporary worktree is created at detached HEAD
	// 4. In the worktree, PullBranch is called for "main"
	// 5. PullBranch does update-ref to update refs/heads/main globally
	// 6. But the hard reset only happens if the WORKTREE is on main (it's not - it's detached)
	// 7. Result: Main workspace now has index/working tree at OLD commit,
	//    but HEAD points to NEW commit - appearing as INVERSE staged changes
	//
	// Root cause: update-ref is global but the index sync is local to the worktree.

	// 1. Setup a "remote" repository with initial content
	remoteScene := testhelpers.NewScene(t, func(s *testhelpers.Scene) error {
		return s.Repo.CreateChangeAndCommit("initial content", "shared")
	})
	remotePath, err := remoteScene.Repo.CreateBareRemote("upstream")
	require.NoError(t, err)
	err = remoteScene.Repo.PushBranch("upstream", "main")
	require.NoError(t, err)

	// 2. Clone to local repository (this will be the "main workspace")
	localDir, err := os.MkdirTemp("", "stackit-test-main-*")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(localDir) })

	cmd := exec.Command("git", "clone", "--branch", "main", remotePath, localDir)
	err = cmd.Run()
	require.NoError(t, err)

	// Create runner for main workspace
	mainRunner := git.NewRunnerWithPath(localDir, nil)
	err = mainRunner.InitDefaultRepo()
	require.NoError(t, err)

	// 3. Simulate a PR: MODIFY the existing shared file on remote
	err = remoteScene.Repo.CreateAndCheckoutBranch("feature")
	require.NoError(t, err)
	// Modify the existing shared_test.txt file with new content
	err = remoteScene.Repo.CreateChangeAndCommit("modified content from PR", "shared")
	require.NoError(t, err)

	// Merge the feature into main on remote
	err = remoteScene.Repo.CheckoutBranch("main")
	require.NoError(t, err)
	err = remoteScene.Repo.RunGitCommand("merge", "--no-ff", "feature", "-m", "Merge PR #528")
	require.NoError(t, err)
	err = remoteScene.Repo.PushBranch("upstream", "main")
	require.NoError(t, err)

	ctx := context.Background()

	// 4. Main workspace is CLEAN - no local changes (this is the key difference!)
	// The user was on main with no uncommitted changes when they ran the merge
	statusBefore, err := mainRunner.GetStatusPorcelain(ctx)
	require.NoError(t, err)
	require.Empty(t, statusBefore, "Main workspace should be clean before worktree pull")

	// Record the current HEAD before pull
	headBefore, err := mainRunner.RunGitCommandWithContext(ctx, "rev-parse", "HEAD")
	require.NoError(t, err)

	// 5. Create a temporary worktree at detached HEAD (simulating merge worktree)
	worktreeDir, err := os.MkdirTemp("", "stackit-test-worktree-*")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(worktreeDir) })

	_, err = mainRunner.RunGitCommandWithContext(ctx, "worktree", "add", "--detach", worktreeDir, "HEAD")
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = mainRunner.RunGitCommandWithContext(ctx, "worktree", "remove", "--force", worktreeDir)
	})

	// Create runner for worktree
	worktreeRunner := git.NewRunnerWithPath(worktreeDir, nil)
	err = worktreeRunner.InitDefaultRepo()
	require.NoError(t, err)

	// Verify worktree is on detached HEAD
	worktreeBranch, err := worktreeRunner.RunGitCommandWithContext(ctx, "rev-parse", "--abbrev-ref", "HEAD")
	require.NoError(t, err)
	require.Equal(t, "HEAD", worktreeBranch, "Worktree should be on detached HEAD")

	// 6. Pull trunk in the worktree - THIS IS WHERE THE BUG HAPPENS
	result, err := worktreeRunner.PullBranch(ctx, "origin", "main")
	require.NoError(t, err)
	require.Equal(t, git.PullDone, result, "PullBranch should succeed")

	// Record HEAD after pull
	headAfter, err := mainRunner.RunGitCommandWithContext(ctx, "rev-parse", "HEAD")
	require.NoError(t, err)
	require.NotEqual(t, headBefore, headAfter, "HEAD should have changed after pull (update-ref worked)")

	// 7. Check main workspace status - THIS IS WHERE THE BUG MANIFESTS
	// BUG: The index/working tree are still at the OLD commit, but HEAD points to NEW commit
	// Git interprets this as: staged changes that UNDO the new content (inverse of the PR)
	statusAfter, err := mainRunner.GetStatusPorcelain(ctx)
	require.NoError(t, err)

	t.Logf("Status before worktree pull: %q", statusBefore)
	t.Logf("Status after worktree pull: %q", statusAfter)
	t.Logf("HEAD before: %s, HEAD after: %s", headBefore[:7], headAfter[:7])

	// The main workspace should remain clean after the worktree pull.
	// Either:
	// a) The index/working tree are updated to match the new HEAD, OR
	// b) The update-ref should not affect the main workspace at all
	//
	// BUG behavior: statusAfter contains "M  shared_test.txt" (staged modification)
	// that represents the INVERSE of the PR (old content vs new HEAD)
	require.Empty(t, statusAfter,
		"Main workspace should be clean after worktree pull, but got inverse changes: %q", statusAfter)
}

func TestPullBranch_WorkingDirSyncWhenOnBranch(t *testing.T) {
	// This test reproduces a bug where PullBranch leaves the working directory
	// with uncommitted changes after pulling when we're already on the branch.
	// The changes appear as the inverse of what was just pulled (reverting the merge).
	//
	// Root cause: After update-ref, using git checkout <branch> is a no-op when
	// we're already on that branch - it doesn't sync the index/working tree.

	// 1. Setup a "remote" repository with initial content
	remoteScene := testhelpers.NewScene(t, func(s *testhelpers.Scene) error {
		return s.Repo.CreateChangeAndCommit("initial content", "init")
	})
	remotePath, err := remoteScene.Repo.CreateBareRemote("upstream")
	require.NoError(t, err)
	err = remoteScene.Repo.PushBranch("upstream", "main")
	require.NoError(t, err)

	// 2. Clone to local repository
	localDir, err := os.MkdirTemp("", "stackit-test-local-*")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(localDir) })

	cmd := exec.Command("git", "clone", "--branch", "main", remotePath, localDir)
	err = cmd.Run()
	require.NoError(t, err)

	// Create runner for local repo
	runner := git.NewRunnerWithPath(localDir, nil)
	err = runner.InitDefaultRepo()
	require.NoError(t, err)

	// 3. Simulate a PR merge on the remote: create feature branch, modify file, merge
	err = remoteScene.Repo.CreateAndCheckoutBranch("feature")
	require.NoError(t, err)
	err = remoteScene.Repo.CreateChangeAndCommit("feature changes", "feature")
	require.NoError(t, err)

	err = remoteScene.Repo.CheckoutBranch("main")
	require.NoError(t, err)
	err = remoteScene.Repo.RunGitCommand("merge", "--no-ff", "feature", "-m", "Merge feature")
	require.NoError(t, err)
	err = remoteScene.Repo.PushBranch("upstream", "main")
	require.NoError(t, err)

	// 4. Local is on main - pull the changes
	ctx := context.Background()
	currentBranch, err := runner.RunGitCommandWithContext(ctx, "rev-parse", "--abbrev-ref", "HEAD")
	require.NoError(t, err)
	require.Equal(t, "main", currentBranch, "Should be on main before pulling")

	result, err := runner.PullBranch(ctx, "origin", "main")
	require.NoError(t, err)
	require.Equal(t, git.PullDone, result, "PullBranch should succeed")

	// 5. Verify working directory is clean - this is the key assertion
	// BUG: Without the fix, git status shows uncommitted changes that revert the merge
	status, err := runner.GetStatusPorcelain(ctx)
	require.NoError(t, err)
	require.Empty(t, status, "Working directory should be clean after pull, but got: %s", status)
}
