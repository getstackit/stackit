package git_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"stackit.dev/stackit/internal/git"
	"stackit.dev/stackit/testhelpers"
)

func TestIsDiffEmpty(t *testing.T) {
	t.Run("returns true when branch equals base", func(t *testing.T) {
		scene := testhelpers.NewScene(t, func(s *testhelpers.Scene) error {
			return s.Repo.CreateChangeAndCommit("initial", "init")
		})

		// Initialize git repo
		err := git.InitDefaultRepoInDir(scene.Dir)
		require.NoError(t, err)

		// Get main revision
		mainRev, err := scene.Repo.GetRef("main")
		require.NoError(t, err)

		// Branch with no changes should be empty
		empty, err := git.IsDiffEmpty(context.Background(), "main", mainRev)
		require.NoError(t, err)
		require.True(t, empty)
	})

	t.Run("returns false when branch has changes", func(t *testing.T) {
		scene := testhelpers.NewScene(t, func(s *testhelpers.Scene) error {
			return s.Repo.CreateChangeAndCommit("initial", "init")
		})

		// Initialize git repo
		err := git.InitDefaultRepoInDir(scene.Dir)
		require.NoError(t, err)

		// Get main revision
		mainRev, err := scene.Repo.GetRef("main")
		require.NoError(t, err)

		// Create branch with changes
		err = scene.Repo.CreateAndCheckoutBranch("branch1")
		require.NoError(t, err)
		err = scene.Repo.CreateChangeAndCommit("branch1 change", "b1")
		require.NoError(t, err)

		// Branch should not be empty
		empty, err := git.IsDiffEmpty(context.Background(), "branch1", mainRev)
		require.NoError(t, err)
		require.False(t, empty)
	})

	t.Run("returns true for branch with no commits", func(t *testing.T) {
		scene := testhelpers.NewScene(t, func(s *testhelpers.Scene) error {
			return s.Repo.CreateChangeAndCommit("initial", "init")
		})

		// Initialize git repo
		err := git.InitDefaultRepoInDir(scene.Dir)
		require.NoError(t, err)

		// Get main revision
		mainRev, err := scene.Repo.GetRef("main")
		require.NoError(t, err)

		// Create branch but don't commit
		err = scene.Repo.CreateAndCheckoutBranch("branch1")
		require.NoError(t, err)
		err = scene.Repo.CheckoutBranch("main")
		require.NoError(t, err)

		// Branch with no commits should be empty
		empty, err := git.IsDiffEmpty(context.Background(), "branch1", mainRev)
		require.NoError(t, err)
		require.True(t, empty)
	})
}

func TestGetUnmergedFiles(t *testing.T) {
	t.Run("returns empty list when no conflicts", func(t *testing.T) {
		_ = testhelpers.NewScene(t, func(s *testhelpers.Scene) error {
			return s.Repo.CreateChangeAndCommit("initial", "init")
		})

		files, err := git.GetUnmergedFiles(context.Background())
		require.NoError(t, err)
		require.Empty(t, files)
	})

	t.Run("returns unmerged files during conflict", func(t *testing.T) {
		scene := testhelpers.NewScene(t, func(s *testhelpers.Scene) error {
			// Create initial file that will be modified to create conflict
			return s.Repo.CreateChangeAndCommit("initial content", "conflict")
		})

		// Get the fork point before branching
		forkPoint, err := scene.Repo.GetRef("main")
		require.NoError(t, err)

		// Create branch with change to conflict file
		err = scene.Repo.CreateAndCheckoutBranch("branch1")
		require.NoError(t, err)
		err = scene.Repo.CreateChange("branch1 change", "conflict", false)
		require.NoError(t, err)
		err = scene.Repo.CreateChangeAndCommit("branch1 change", "b1")
		require.NoError(t, err)

		// Create conflicting change in main
		err = scene.Repo.CheckoutBranch("main")
		require.NoError(t, err)
		err = scene.Repo.CreateChange("main conflicting", "conflict", false)
		require.NoError(t, err)
		err = scene.Repo.CreateChangeAndCommit("main conflicting", "main")
		require.NoError(t, err)

		// Start rebase (will conflict)
		_, err = git.Rebase(context.Background(), "branch1", "main", forkPoint)
		require.NoError(t, err)

		// Should have unmerged files
		files, err := git.GetUnmergedFiles(context.Background())
		require.NoError(t, err)
		require.NotEmpty(t, files)
		require.Contains(t, files, "conflict_test.txt")
	})
}
