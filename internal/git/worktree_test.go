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

func TestWorktree(t *testing.T) {
	t.Run("add and remove worktree", func(t *testing.T) {
		scene := testhelpers.NewScene(t, testhelpers.InitialCommitSceneSetup)
		runner := git.NewRunnerWithPath(scene.Repo.Dir, nil)

		// Create a branch to checkout in the worktree
		err := scene.Repo.CreateBranch("test-branch")
		require.NoError(t, err)

		// Create a temporary directory for the worktree
		tmpDir := t.TempDir()

		// Normalize worktree path (on macOS /var is symlinked to /private/var)
		worktreePath, err := filepath.EvalSymlinks(tmpDir)
		require.NoError(t, err)
		worktreePath = filepath.Join(worktreePath, "worktree")

		// Add worktree
		err = runner.AddWorktree(context.Background(), worktreePath, "test-branch", git.WorktreeAttached)
		require.NoError(t, err)

		// Verify worktree exists
		_, err = os.Stat(filepath.Join(worktreePath, ".git"))
		require.NoError(t, err)

		// List worktrees
		worktrees, err := runner.ListWorktrees(context.Background())
		require.NoError(t, err)
		require.Contains(t, worktrees.Paths(), worktreePath)

		// Remove worktree
		err = runner.RemoveWorktree(context.Background(), worktreePath)
		require.NoError(t, err)

		// Verify worktree is gone from list
		worktrees, err = runner.ListWorktrees(context.Background())
		require.NoError(t, err)
		require.NotContains(t, worktrees.Paths(), worktreePath)
	})

	t.Run("add detached worktree", func(t *testing.T) {
		scene := testhelpers.NewScene(t, testhelpers.InitialCommitSceneSetup)
		runner := git.NewRunnerWithPath(scene.Repo.Dir, nil)

		// Create a temporary directory for the worktree
		tmpDir := t.TempDir()

		worktreePath := filepath.Join(tmpDir, "worktree-detached")

		// Add detached worktree
		err := runner.AddWorktree(context.Background(), worktreePath, "", git.WorktreeDetached)
		require.NoError(t, err)

		// Verify worktree exists
		_, err = os.Stat(filepath.Join(worktreePath, ".git"))
		require.NoError(t, err)

		// Clean up
		err = runner.RemoveWorktree(context.Background(), worktreePath)
		require.NoError(t, err)
	})
}

func TestWorktreeRegistry(t *testing.T) {
	t.Run("write and read worktree metadata", func(t *testing.T) {
		scene := testhelpers.NewScene(t, testhelpers.InitialCommitSceneSetup)
		runner := git.NewRunnerWithPath(scene.Repo.Dir, nil)

		// Write worktree metadata
		meta := &git.WorktreeMeta{
			Path:         "/path/to/worktree",
			AnchorBranch: "feature-branch",
			MainRepoDir:  scene.Repo.Dir,
		}
		err := runner.WriteWorktreeMeta("feature-branch", meta)
		require.NoError(t, err)

		// Read it back
		readMeta, err := runner.ReadWorktreeMeta("feature-branch")
		require.NoError(t, err)
		require.NotNil(t, readMeta)
		require.Equal(t, "/path/to/worktree", readMeta.Path)
		require.Equal(t, "feature-branch", readMeta.AnchorBranch)
		require.Equal(t, scene.Repo.Dir, readMeta.MainRepoDir)
	})

	t.Run("read non-existent worktree metadata returns nil", func(t *testing.T) {
		scene := testhelpers.NewScene(t, testhelpers.InitialCommitSceneSetup)
		runner := git.NewRunnerWithPath(scene.Repo.Dir, nil)

		// Read non-existent metadata
		meta, err := runner.ReadWorktreeMeta("non-existent")
		require.NoError(t, err)
		require.Nil(t, meta)
	})

	t.Run("delete worktree metadata", func(t *testing.T) {
		scene := testhelpers.NewScene(t, testhelpers.InitialCommitSceneSetup)
		runner := git.NewRunnerWithPath(scene.Repo.Dir, nil)

		// Write worktree metadata
		meta := &git.WorktreeMeta{
			Path:         "/path/to/worktree",
			AnchorBranch: "feature-branch",
		}
		err := runner.WriteWorktreeMeta("feature-branch", meta)
		require.NoError(t, err)

		// Delete it
		err = runner.DeleteWorktreeMeta(context.Background(), "feature-branch")
		require.NoError(t, err)

		// Verify it's gone
		readMeta, err := runner.ReadWorktreeMeta("feature-branch")
		require.NoError(t, err)
		require.Nil(t, readMeta)
	})

	t.Run("list worktree metadata", func(t *testing.T) {
		scene := testhelpers.NewScene(t, testhelpers.InitialCommitSceneSetup)
		runner := git.NewRunnerWithPath(scene.Repo.Dir, nil)

		// Write multiple worktree metadata
		meta1 := &git.WorktreeMeta{
			Path:         "/path/to/worktree1",
			AnchorBranch: "feature-1",
		}
		err := runner.WriteWorktreeMeta("feature-1", meta1)
		require.NoError(t, err)

		meta2 := &git.WorktreeMeta{
			Path:         "/path/to/worktree2",
			AnchorBranch: "feature-2",
		}
		err = runner.WriteWorktreeMeta("feature-2", meta2)
		require.NoError(t, err)

		// List all
		metas, err := runner.ListWorktreeMetas()
		require.NoError(t, err)
		require.Len(t, metas, 2)
		require.Contains(t, metas, "feature-1")
		require.Contains(t, metas, "feature-2")
		require.Equal(t, "/path/to/worktree1", metas["feature-1"].Path)
		require.Equal(t, "/path/to/worktree2", metas["feature-2"].Path)
	})
}
