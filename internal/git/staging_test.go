package git_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/testhelpers"
)

func TestStageAll(t *testing.T) {
	t.Run("stages all changes including untracked", func(t *testing.T) {
		scene := testhelpers.NewScene(t, testhelpers.InitialCommitSceneSetup)
		runner := git.NewRunnerWithPath(scene.Dir, nil)

		// Create unstaged change
		err := scene.Repo.CreateChange("new content", "test", true)
		require.NoError(t, err)

		// Create untracked file
		err = scene.Repo.CreateChange("untracked", "newfile", true)
		require.NoError(t, err)

		// Verify no staged changes initially
		hasStaged, err := runner.HasStagedChanges(context.Background())
		require.NoError(t, err)
		require.False(t, hasStaged)

		// Stage all
		err = runner.StageAll(context.Background())
		require.NoError(t, err)

		// Verify changes are staged
		hasStaged, err = runner.HasStagedChanges(context.Background())
		require.NoError(t, err)
		require.True(t, hasStaged)
	})
}

func TestStageTracked(t *testing.T) {
	t.Run("stages only tracked file changes", func(t *testing.T) {
		scene := testhelpers.NewScene(t, func(s *testhelpers.Scene) error {
			// Create initial tracked file
			return s.Repo.CreateChangeAndCommit("initial", "test")
		})
		runner := git.NewRunnerWithPath(scene.Dir, nil)

		// Modify tracked file (unstaged)
		err := scene.Repo.CreateChange("modified", "test", true)
		require.NoError(t, err)

		// Create untracked file
		err = scene.Repo.CreateChange("untracked", "newfile", true)
		require.NoError(t, err)

		// Stage tracked only
		err = runner.StageTracked(context.Background())
		require.NoError(t, err)

		// Verify tracked file is staged
		hasStaged, err := runner.HasStagedChanges(context.Background())
		require.NoError(t, err)
		require.True(t, hasStaged)
	})
}

func TestHasStagedChanges(t *testing.T) {
	t.Run("returns false when no staged changes", func(t *testing.T) {
		scene := testhelpers.NewScene(t, testhelpers.InitialCommitSceneSetup)
		runner := git.NewRunnerWithPath(scene.Dir, nil)

		hasStaged, err := runner.HasStagedChanges(context.Background())
		require.NoError(t, err)
		require.False(t, hasStaged)
	})

	t.Run("returns true when changes are staged", func(t *testing.T) {
		scene := testhelpers.NewScene(t, testhelpers.InitialCommitSceneSetup)
		runner := git.NewRunnerWithPath(scene.Dir, nil)

		// Create and stage change
		err := scene.Repo.CreateChange("new content", "test", false)
		require.NoError(t, err)

		hasStaged, err := runner.HasStagedChanges(context.Background())
		require.NoError(t, err)
		require.True(t, hasStaged)
	})
}

func TestHasUnstagedChanges(t *testing.T) {
	t.Run("returns false when no unstaged changes", func(t *testing.T) {
		scene := testhelpers.NewScene(t, testhelpers.InitialCommitSceneSetup)
		runner := git.NewRunnerWithPath(scene.Dir, nil)

		hasUnstaged, err := runner.HasUnstagedChanges(context.Background())
		require.NoError(t, err)
		require.False(t, hasUnstaged)
	})

	t.Run("returns true when unstaged changes exist", func(t *testing.T) {
		scene := testhelpers.NewScene(t, func(s *testhelpers.Scene) error {
			return s.Repo.CreateChangeAndCommit("initial", "test")
		})
		runner := git.NewRunnerWithPath(scene.Dir, nil)

		// Create unstaged change
		err := scene.Repo.CreateChange("modified", "test", true)
		require.NoError(t, err)

		hasUnstaged, err := runner.HasUnstagedChanges(context.Background())
		require.NoError(t, err)
		require.True(t, hasUnstaged)
	})
}

func TestHasUntrackedFiles(t *testing.T) {
	t.Run("returns false when no untracked files", func(t *testing.T) {
		scene := testhelpers.NewScene(t, testhelpers.InitialCommitSceneSetup)
		runner := git.NewRunnerWithPath(scene.Dir, nil)

		hasUntracked, err := runner.HasUntrackedFiles(context.Background())
		require.NoError(t, err)
		require.False(t, hasUntracked)
	})

	t.Run("returns true when untracked files exist", func(t *testing.T) {
		scene := testhelpers.NewScene(t, testhelpers.InitialCommitSceneSetup)
		runner := git.NewRunnerWithPath(scene.Dir, nil)

		// Create untracked file
		err := scene.Repo.CreateChange("content", "newfile", true)
		require.NoError(t, err)

		hasUntracked, err := runner.HasUntrackedFiles(context.Background())
		require.NoError(t, err)
		require.True(t, hasUntracked)
	})
}

func TestAddAll(t *testing.T) {
	t.Run("is alias for StageAll", func(t *testing.T) {
		scene := testhelpers.NewScene(t, testhelpers.InitialCommitSceneSetup)
		runner := git.NewRunnerWithPath(scene.Dir, nil)

		// Create unstaged change
		err := scene.Repo.CreateChange("new content", "test", true)
		require.NoError(t, err)

		// Use AddAll
		err = runner.AddAll(context.Background())
		require.NoError(t, err)

		// Verify changes are staged
		hasStaged, err := runner.HasStagedChanges(context.Background())
		require.NoError(t, err)
		require.True(t, hasStaged)
	})
}
