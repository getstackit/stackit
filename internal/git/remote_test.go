package git_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"stackit.dev/stackit/internal/git"
	"stackit.dev/stackit/testhelpers"
)

func TestFetchRemoteShas(t *testing.T) {
	t.Run("fetches SHAs from remote", func(t *testing.T) {
		scene := testhelpers.NewScene(t, func(s *testhelpers.Scene) error {
			return s.Repo.CreateChangeAndCommit("initial", "init")
		})

		// Create a bare remote
		_, err := scene.Repo.CreateBareRemote("origin")
		require.NoError(t, err)

		// Push main
		err = scene.Repo.PushBranch("origin", "main")
		require.NoError(t, err)

		// Create and push feature branch
		err = scene.Repo.CreateAndCheckoutBranch("feature")
		require.NoError(t, err)
		err = scene.Repo.CreateChangeAndCommit("feature change", "feat")
		require.NoError(t, err)
		err = scene.Repo.PushBranch("origin", "feature")
		require.NoError(t, err)

		// Fetch remote SHAs (FetchRemoteShas runs git command in current dir which is scene.Dir)
		git.SetWorkingDir(scene.Dir)
		err = git.InitDefaultRepoInDir(scene.Dir)
		require.NoError(t, err)

		remoteShas, err := git.FetchRemoteShas("origin")
		require.NoError(t, err)

		// Should have both branches
		require.Contains(t, remoteShas, "main")
		require.Contains(t, remoteShas, "feature")

		// SHAs should be valid (40 hex characters)
		require.Len(t, remoteShas["main"], 40)
		require.Len(t, remoteShas["feature"], 40)

		// SHAs should be different
		require.NotEqual(t, remoteShas["main"], remoteShas["feature"])
	})

	t.Run("returns empty map for empty remote", func(t *testing.T) {
		scene := testhelpers.NewScene(t, func(s *testhelpers.Scene) error {
			return s.Repo.CreateChangeAndCommit("initial", "init")
		})

		// Create a bare remote but don't push anything
		_, err := scene.Repo.CreateBareRemote("origin")
		require.NoError(t, err)

		// Fetch remote SHAs - should be empty since nothing was pushed
		git.SetWorkingDir(scene.Dir)
		err = git.InitDefaultRepoInDir(scene.Dir)
		require.NoError(t, err)

		remoteShas, err := git.FetchRemoteShas("origin")
		require.NoError(t, err)
		require.Empty(t, remoteShas, "remote should have no branches")
	})

	t.Run("handles branches with slashes in names", func(t *testing.T) {
		scene := testhelpers.NewScene(t, func(s *testhelpers.Scene) error {
			return s.Repo.CreateChangeAndCommit("initial", "init")
		})

		// Create a bare remote
		_, err := scene.Repo.CreateBareRemote("origin")
		require.NoError(t, err)

		// Push main
		err = scene.Repo.PushBranch("origin", "main")
		require.NoError(t, err)

		// Create branch with slash in name
		err = scene.Repo.RunGitCommand("checkout", "-b", "feature/my-feature")
		require.NoError(t, err)
		err = scene.Repo.CreateChangeAndCommit("feature change", "feat")
		require.NoError(t, err)
		err = scene.Repo.PushBranch("origin", "feature/my-feature")
		require.NoError(t, err)

		// Fetch remote SHAs
		git.SetWorkingDir(scene.Dir)
		err = git.InitDefaultRepoInDir(scene.Dir)
		require.NoError(t, err)

		remoteShas, err := git.FetchRemoteShas("origin")
		require.NoError(t, err)

		// Should have the branch with slash
		require.Contains(t, remoteShas, "feature/my-feature")
	})
}
