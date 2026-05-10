package git_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/testhelpers"
)

func TestGetRepoInfo(t *testing.T) {
	t.Run("parses SCP-style SSH URL", func(t *testing.T) {
		scene := testhelpers.NewScene(t, testhelpers.InitialCommitSceneSetup)

		// Set remote URL to SCP-style SSH format
		err := scene.Repo.RunGitCommand("config", "remote.origin.url", "git@github.com:myowner/myrepo.git")
		require.NoError(t, err)

		runner := git.NewRunnerWithPath(scene.Dir, nil)
		owner, repo, err := runner.GetRepoInfo(context.Background())
		require.NoError(t, err)
		require.Equal(t, "myowner", owner)
		require.Equal(t, "myrepo", repo)
	})

	t.Run("parses ssh:// protocol SSH URL", func(t *testing.T) {
		scene := testhelpers.NewScene(t, testhelpers.InitialCommitSceneSetup)

		// Set remote URL to ssh:// protocol format
		err := scene.Repo.RunGitCommand("config", "remote.origin.url", "ssh://git@github.com/myowner/myrepo.git")
		require.NoError(t, err)

		runner := git.NewRunnerWithPath(scene.Dir, nil)
		owner, repo, err := runner.GetRepoInfo(context.Background())
		require.NoError(t, err)
		require.Equal(t, "myowner", owner)
		require.Equal(t, "myrepo", repo)
	})

	t.Run("parses ssh:// protocol SSH URL without .git suffix", func(t *testing.T) {
		scene := testhelpers.NewScene(t, testhelpers.InitialCommitSceneSetup)

		err := scene.Repo.RunGitCommand("config", "remote.origin.url", "ssh://git@github.com/myowner/myrepo")
		require.NoError(t, err)

		runner := git.NewRunnerWithPath(scene.Dir, nil)
		owner, repo, err := runner.GetRepoInfo(context.Background())
		require.NoError(t, err)
		require.Equal(t, "myowner", owner)
		require.Equal(t, "myrepo", repo)
	})

	t.Run("parses ssh:// protocol with GitHub Enterprise", func(t *testing.T) {
		scene := testhelpers.NewScene(t, testhelpers.InitialCommitSceneSetup)

		err := scene.Repo.RunGitCommand("config", "remote.origin.url", "ssh://git@github.enterprise.com/org/project.git")
		require.NoError(t, err)

		runner := git.NewRunnerWithPath(scene.Dir, nil)
		owner, repo, err := runner.GetRepoInfo(context.Background())
		require.NoError(t, err)
		require.Equal(t, "org", owner)
		require.Equal(t, "project", repo)
	})

	t.Run("parses HTTPS URL", func(t *testing.T) {
		scene := testhelpers.NewScene(t, testhelpers.InitialCommitSceneSetup)

		err := scene.Repo.RunGitCommand("config", "remote.origin.url", "https://github.com/myowner/myrepo.git")
		require.NoError(t, err)

		runner := git.NewRunnerWithPath(scene.Dir, nil)
		owner, repo, err := runner.GetRepoInfo(context.Background())
		require.NoError(t, err)
		require.Equal(t, "myowner", owner)
		require.Equal(t, "myrepo", repo)
	})

	t.Run("returns empty for missing remote", func(t *testing.T) {
		scene := testhelpers.NewScene(t, testhelpers.InitialCommitSceneSetup)

		// Don't set any remote URL
		runner := git.NewRunnerWithPath(scene.Dir, nil)
		owner, repo, err := runner.GetRepoInfo(context.Background())
		require.NoError(t, err)
		require.Empty(t, owner)
		require.Empty(t, repo)
	})

	t.Run("parses HTTPS URL with authentication", func(t *testing.T) {
		scene := testhelpers.NewScene(t, testhelpers.InitialCommitSceneSetup)

		// HTTPS URL with embedded username (e.g., for token auth)
		err := scene.Repo.RunGitCommand("config", "remote.origin.url", "https://user@github.com/myowner/myrepo.git")
		require.NoError(t, err)

		runner := git.NewRunnerWithPath(scene.Dir, nil)
		owner, repo, err := runner.GetRepoInfo(context.Background())
		require.NoError(t, err)
		require.Equal(t, "myowner", owner)
		require.Equal(t, "myrepo", repo)
	})

	t.Run("parses ssh:// URL with explicit port", func(t *testing.T) {
		scene := testhelpers.NewScene(t, testhelpers.InitialCommitSceneSetup)

		// SSH URL with explicit port number
		err := scene.Repo.RunGitCommand("config", "remote.origin.url", "ssh://git@github.com:22/myowner/myrepo.git")
		require.NoError(t, err)

		runner := git.NewRunnerWithPath(scene.Dir, nil)
		owner, repo, err := runner.GetRepoInfo(context.Background())
		require.NoError(t, err)
		require.Equal(t, "myowner", owner)
		require.Equal(t, "myrepo", repo)
	})

	t.Run("parses git:// protocol URL", func(t *testing.T) {
		scene := testhelpers.NewScene(t, testhelpers.InitialCommitSceneSetup)

		// git:// protocol (read-only anonymous access)
		err := scene.Repo.RunGitCommand("config", "remote.origin.url", "git://github.com/myowner/myrepo.git")
		require.NoError(t, err)

		runner := git.NewRunnerWithPath(scene.Dir, nil)
		owner, repo, err := runner.GetRepoInfo(context.Background())
		require.NoError(t, err)
		require.Equal(t, "myowner", owner)
		require.Equal(t, "myrepo", repo)
	})
}
