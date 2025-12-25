package git_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"stackit.dev/stackit/internal/git"
	"stackit.dev/stackit/testhelpers"
)

func TestIsMerged(t *testing.T) {
	t.Run("returns false for unmerged branch", func(t *testing.T) {
		scene := testhelpers.NewScene(t, func(s *testhelpers.Scene) error {
			return s.Repo.CreateChangeAndCommit("initial", "init")
		})

		// Create branch
		err := scene.Repo.CreateAndCheckoutBranch("branch1")
		require.NoError(t, err)
		err = scene.Repo.CreateChangeAndCommit("branch1 change", "b1")
		require.NoError(t, err)
		err = scene.Repo.CheckoutBranch("main")
		require.NoError(t, err)

		// Initialize git repo
		err = git.InitDefaultRepoInDir(scene.Dir)
		require.NoError(t, err)

		// Branch is not merged
		merged, err := git.IsMerged(context.Background(), "branch1", "main")
		require.NoError(t, err)
		require.False(t, merged)
	})

	t.Run("returns true for merged branch", func(t *testing.T) {
		scene := testhelpers.NewScene(t, func(s *testhelpers.Scene) error {
			return s.Repo.CreateChangeAndCommit("initial", "init")
		})

		// Create branch
		err := scene.Repo.CreateAndCheckoutBranch("branch1")
		require.NoError(t, err)
		err = scene.Repo.CreateChangeAndCommit("branch1 change", "b1")
		require.NoError(t, err)

		// Merge branch into main
		err = scene.Repo.CheckoutBranch("main")
		require.NoError(t, err)
		err = scene.Repo.MergeBranch("main", "branch1")
		require.NoError(t, err)

		// Initialize git repo
		err = git.InitDefaultRepoInDir(scene.Dir)
		require.NoError(t, err)

		// Branch should be merged
		merged, err := git.IsMerged(context.Background(), "branch1", "main")
		require.NoError(t, err)
		require.True(t, merged)
	})
}
