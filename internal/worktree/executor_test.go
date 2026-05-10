package worktree

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/output"
	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

func TestExecutor_CreateSession(t *testing.T) {
	t.Run("creates worktree at trunk", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit()

		executor := NewExecutor(s.Engine, output.NewNullOutput())

		session, err := executor.CreateSession(context.Background(), CreateSessionOptions{
			NamePattern: "test-worktree-*",
		})
		require.NoError(t, err)
		defer session.Close()

		// Verify worktree exists
		assert.DirExists(t, session.Path)

		// Verify engine is usable
		trunk := session.Engine.Trunk()
		assert.Equal(t, "main", trunk.GetName())
	})

	t.Run("creates worktree at specific ref", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit()

		// Create a branch
		s.Scene.Repo.CreateAndCheckoutBranch("feature")
		s.Scene.Repo.CreateChangeAndCommit("feature change", "feature")
		s.Scene.Repo.CheckoutBranch("main")

		executor := NewExecutor(s.Engine, output.NewNullOutput())

		session, err := executor.CreateSession(context.Background(), CreateSessionOptions{
			Ref:         "feature",
			NamePattern: "test-worktree-*",
		})
		require.NoError(t, err)
		defer session.Close()

		// Verify worktree is at feature branch (file is named prefix_test.txt)
		assert.DirExists(t, session.Path)
		featureFile := filepath.Join(session.Path, "feature_test.txt")
		assert.FileExists(t, featureFile)
	})

	t.Run("cleanup removes worktree", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit()

		executor := NewExecutor(s.Engine, output.NewNullOutput())

		session, err := executor.CreateSession(context.Background(), CreateSessionOptions{
			NamePattern: "test-worktree-*",
		})
		require.NoError(t, err)

		worktreePath := session.Path
		assert.DirExists(t, worktreePath)

		session.Close()

		// Worktree should be removed
		_, err = os.Stat(worktreePath)
		assert.True(t, os.IsNotExist(err), "worktree should be removed after Close()")
	})
}

func TestSession_ResetToTrunk(t *testing.T) {
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
	s.WithInitialCommit()

	executor := NewExecutor(s.Engine, output.NewNullOutput())

	session, err := executor.CreateSession(context.Background(), CreateSessionOptions{
		NamePattern: "test-worktree-*",
	})
	require.NoError(t, err)
	defer session.Close()

	// Get initial revision
	initialRev, err := session.GetCurrentRevision(context.Background())
	require.NoError(t, err)

	// Make a change in the worktree
	testFile := filepath.Join(session.Path, "test.txt")
	err = os.WriteFile(testFile, []byte("test"), 0644)
	require.NoError(t, err)

	// Stage and commit using the worktree engine's git
	git := session.Engine.Git()
	_, err = git.RunGitCommandWithContext(context.Background(), "add", ".")
	require.NoError(t, err)
	_, err = git.RunGitCommandWithContext(context.Background(), "commit", "-m", "test commit")
	require.NoError(t, err)

	// Verify we're at a different revision
	newRev, err := session.GetCurrentRevision(context.Background())
	require.NoError(t, err)
	assert.NotEqual(t, initialRev, newRev)

	// Reset to trunk
	err = session.ResetToTrunk(context.Background())
	require.NoError(t, err)

	// Verify we're back at initial revision
	afterResetRev, err := session.GetCurrentRevision(context.Background())
	require.NoError(t, err)
	assert.Equal(t, initialRev, afterResetRev)
}

func TestSession_ResetToRef(t *testing.T) {
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
	s.WithInitialCommit()

	// Create a branch with a commit
	s.Scene.Repo.CreateAndCheckoutBranch("feature")
	s.Scene.Repo.CreateChangeAndCommit("feature change", "feature")

	// Get the feature SHA
	featureSHA, err := s.Scene.Repo.GetCurrentSHA()
	require.NoError(t, err)

	s.Scene.Repo.CheckoutBranch("main")

	// Rebuild engine to pick up the new branch
	eng, err := engine.NewEngine(engine.Options{
		RepoRoot: s.Scene.Dir,
		Trunk:    "main",
	})
	require.NoError(t, err)

	executor := NewExecutor(eng, output.NewNullOutput())

	session, err := executor.CreateSession(context.Background(), CreateSessionOptions{
		NamePattern: "test-worktree-*",
	})
	require.NoError(t, err)
	defer session.Close()

	// Reset to the feature branch
	err = session.ResetToRef(context.Background(), "feature")
	require.NoError(t, err)

	// Verify we're at the feature revision
	afterResetRev, err := session.GetCurrentRevision(context.Background())
	require.NoError(t, err)
	assert.Equal(t, featureSHA, afterResetRev)

	// Verify the feature file exists (file is named prefix_test.txt)
	featureFile := filepath.Join(session.Path, "feature_test.txt")
	assert.FileExists(t, featureFile)
}

func TestSession_PullTrunk_StaysDetached(t *testing.T) {
	// This test verifies that pullTrunk keeps the worktree at detached HEAD
	// and doesn't checkout the trunk branch directly. This is critical because
	// if the worktree checks out main, any subsequent commits (especially merges)
	// would update refs/heads/main globally, affecting the user's main workspace.
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
	s.WithInitialCommit()

	// Create a feature branch and switch to it in the main repo
	// This means main is NOT checked out in the main repo, so a worktree
	// COULD checkout main directly (which would be the bug we're testing against)
	s.Scene.Repo.CreateAndCheckoutBranch("feature")
	s.Scene.Repo.CreateChangeAndCommit("feature change", "feature")

	// Get the main SHA before any worktree operations
	mainSHABefore, err := s.Scene.Repo.RunGitCommandAndGetOutput("rev-parse", "main")
	require.NoError(t, err)

	// Rebuild engine while on feature branch
	eng, err := engine.NewEngine(engine.Options{
		RepoRoot: s.Scene.Dir,
		Trunk:    "main",
	})
	require.NoError(t, err)

	executor := NewExecutor(eng, output.NewNullOutput())

	// Create session - this internally calls pullTrunk which uses ResetHard
	// to stay at detached HEAD instead of CheckoutBranch which would checkout main
	session, err := executor.CreateSession(context.Background(), CreateSessionOptions{
		NamePattern: "test-worktree-*",
		// Note: PullTrunk is false here, but the worktree is created at trunk
	})
	require.NoError(t, err)
	defer session.Close()

	// Verify the worktree is at detached HEAD, not on a branch
	// Get current branch in worktree
	worktreeGit := session.Engine.Git()
	currentBranch, err := worktreeGit.RunGitCommandWithContext(context.Background(), "symbolic-ref", "--short", "HEAD")
	if err == nil {
		// If symbolic-ref succeeds, we're on a branch (which is the bug!)
		t.Errorf("Worktree should be at detached HEAD, but is on branch: %s", currentBranch)
	}
	// If symbolic-ref fails with "not a symbolic ref", we're correctly at detached HEAD

	// Now simulate what would happen if we created a merge commit in the worktree
	// First, create another branch to merge
	_, err = worktreeGit.RunGitCommandWithContext(context.Background(), "branch", "to-merge")
	require.NoError(t, err)
	_, err = worktreeGit.RunGitCommandWithContext(context.Background(), "checkout", "to-merge")
	require.NoError(t, err)

	testFile := filepath.Join(session.Path, "merge-test.txt")
	err = os.WriteFile(testFile, []byte("merge content"), 0644)
	require.NoError(t, err)
	_, err = worktreeGit.RunGitCommandWithContext(context.Background(), "add", ".")
	require.NoError(t, err)
	_, err = worktreeGit.RunGitCommandWithContext(context.Background(), "commit", "-m", "commit to merge")
	require.NoError(t, err)

	// Go back to detached at main's commit
	_, err = worktreeGit.RunGitCommandWithContext(context.Background(), "checkout", "--detach", "main")
	require.NoError(t, err)

	// Merge the branch (this creates a merge commit at detached HEAD)
	_, err = worktreeGit.RunGitCommandWithContext(context.Background(), "merge", "to-merge", "-m", "Test merge")
	require.NoError(t, err)

	// CRITICAL: Verify that main in the MAIN repo was NOT affected
	// If the worktree had been on main instead of detached, this merge would have
	// updated refs/heads/main globally
	mainSHAAfter, err := s.Scene.Repo.RunGitCommandAndGetOutput("rev-parse", "main")
	require.NoError(t, err)

	assert.Equal(t, mainSHABefore, mainSHAAfter,
		"main branch SHA should not change when merging in a detached worktree")
}
