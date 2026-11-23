package git_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/testhelpers"
)

func TestIsDiffEmpty(t *testing.T) {
	t.Parallel()
	t.Run("returns true when branch equals base", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)

		// Initialize git repo
		runner := git.NewRunnerWithPath(scene.Dir, nil)

		// Get main revision
		mainRev, err := scene.Repo.GetRef("main")
		require.NoError(t, err)

		// Branch with no changes should be empty
		diff, err := runner.ReadDiffs(t.Context(), git.DiffCheckOnly, git.RevRange{Base: mainRev, Head: "main"}).One()
		require.NoError(t, err)
		require.True(t, diff.Empty)
	})

	t.Run("returns false when branch has changes", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)

		// Initialize git repo
		runner := git.NewRunnerWithPath(scene.Dir, nil)

		// Get main revision
		mainRev, err := scene.Repo.GetRef("main")
		require.NoError(t, err)

		// Create branch with changes
		err = scene.Repo.CreateAndCheckoutBranch("branch1")
		require.NoError(t, err)
		err = scene.Repo.CreateChangeAndCommit("branch1 change", "b1")
		require.NoError(t, err)

		// Branch should not be empty
		diff, err := runner.ReadDiffs(t.Context(), git.DiffCheckOnly, git.RevRange{Base: mainRev, Head: "branch1"}).One()
		require.NoError(t, err)
		require.False(t, diff.Empty)
	})

	t.Run("returns true for branch with no commits", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)

		// Initialize git repo
		runner := git.NewRunnerWithPath(scene.Dir, nil)

		// Get main revision
		mainRev, err := scene.Repo.GetRef("main")
		require.NoError(t, err)

		// Create branch but don't commit
		err = scene.Repo.CreateAndCheckoutBranch("branch1")
		require.NoError(t, err)
		err = scene.Repo.CheckoutBranch("main")
		require.NoError(t, err)

		// Branch with no commits should be empty
		diff, err := runner.ReadDiffs(t.Context(), git.DiffCheckOnly, git.RevRange{Base: mainRev, Head: "branch1"}).One()
		require.NoError(t, err)
		require.True(t, diff.Empty)
	})
}

func TestGetUnmergedFiles(t *testing.T) {
	t.Parallel()
	t.Run("returns empty list when no conflicts", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)

		runner := git.NewRunnerWithPath(scene.Dir, nil)
		files, err := runner.GetUnmergedFiles(t.Context())
		require.NoError(t, err)
		require.Empty(t, files)
	})

	// git C-quotes paths with spaces or non-ASCII bytes unless -z is used, so
	// cover both a plain name and one that would have come back quoted.
	for _, tc := range []struct {
		name   string
		prefix string
	}{
		{name: "returns unmerged files during conflict", prefix: "conflict"},
		{name: "returns unquoted paths with spaces and non-ASCII characters", prefix: "café conflict"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			scene := testhelpers.NewSceneParallel(t, func(s *testhelpers.Scene) error {
				return s.Repo.CreateChangeAndCommit("initial content", tc.prefix)
			})

			forkPoint, err := scene.Repo.GetRef("main")
			require.NoError(t, err)

			require.NoError(t, scene.Repo.CreateAndCheckoutBranch("branch1"))
			require.NoError(t, scene.Repo.CreateChangeAndCommit("branch1 change", tc.prefix))

			require.NoError(t, scene.Repo.CheckoutBranch("main"))
			require.NoError(t, scene.Repo.CreateChangeAndCommit("main conflicting", tc.prefix))

			runner := git.NewRunnerWithPath(scene.Dir, nil)
			_, err = runner.Rebase(t.Context(), git.RebaseSpec{Branch: "branch1", Onto: "main", OldBase: forkPoint})
			require.NoError(t, err)

			files, err := runner.GetUnmergedFiles(t.Context())
			require.NoError(t, err)
			require.Equal(t, []string{tc.prefix + "_test.txt"}, files)
		})
	}

	t.Run("returns an error outside a repository", func(t *testing.T) {
		t.Parallel()
		runner := git.NewRunnerWithPath(t.TempDir(), nil)
		files, err := runner.GetUnmergedFiles(t.Context())
		require.Error(t, err)
		require.Empty(t, files)
	})
}
