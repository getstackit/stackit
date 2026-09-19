package git_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/testhelpers"
)

func TestReadCommitInfo(t *testing.T) {
	t.Parallel()
	scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)
	require.NoError(t, scene.Repo.CreateAndCheckoutBranch("branch1"))
	require.NoError(t, scene.Repo.CreateChangeAndCommit("branch1 change", "b1"))
	require.NoError(t, scene.Repo.RunGitCommand("tag", "branch1", "main"))
	logger := &traceCaptureLogger{}
	runner := git.NewRunnerWithPath(scene.Dir, logger)
	_, err := runner.RunGitCommandWithEnv(t.Context(), []string{
		"GIT_AUTHOR_NAME=Renée Example", "GIT_AUTHOR_EMAIL=author@example.com",
		"GIT_AUTHOR_DATE=2026-06-02T09:30:00+05:30",
	}, "commit", "--amend", "--no-edit", "--reset-author")
	require.NoError(t, err)
	logger.calls = 0
	refs := []string{"main", "HEAD", "refs/heads/branch1", "refs/tags/branch1", "missing"}
	got := runner.ReadCommitInfo(t.Context(), refs...)
	require.Equal(t, 1, logger.calls, "all commit fields and refs use one subprocess")
	require.Len(t, got.Values, 4)
	require.ErrorContains(t, got.Errors["missing"], "commit not found")
	for ref, info := range got.Values {
		wantDate, err := runner.RunGitCommandWithContext(t.Context(), "log", "-1", "--format=%aI", ref)
		require.NoError(t, err)
		wantAuthor, err := runner.RunGitCommandWithContext(t.Context(), "log", "-1", "--format=%an", ref)
		require.NoError(t, err)
		require.Equal(t, wantDate, info.Date.Format(time.RFC3339))
		require.Equal(t, wantAuthor, info.Author)
	}
	require.Equal(t, got.Values["HEAD"], got.Values["refs/heads/branch1"])
	require.Equal(t, got.Values["main"], got.Values["refs/tags/branch1"])
	require.Empty(t, runner.ReadCommitInfo(t.Context()).Values)
	blob, err := runner.CreateBlob("not a commit")
	require.NoError(t, err)
	mixed := runner.ReadCommitInfo(t.Context(), blob, "HEAD")
	require.Contains(t, mixed.Values, "HEAD")
	require.Contains(t, mixed.Errors, blob)
}

func TestGetRemoteRevision_UsesConfiguredRemote(t *testing.T) {
	t.Parallel()
	scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)

	// Point main's configured remote at a name other than "origin" and stand
	// in a local branch for that remote's tracking ref.
	require.NoError(t, scene.Repo.RunGitCommand("config", "branch.main.remote", "upstream"))
	mainSHA, err := scene.Repo.GetRevision("main")
	require.NoError(t, err)
	require.NoError(t, scene.Repo.RunGitCommand("branch", "upstream/main", mainSHA))

	runner := git.NewRunnerWithPath(scene.Dir, nil)
	got, err := runner.ReadRevisions(context.Background(), runner.GetRemote()+"/"+"main").One()
	require.NoError(t, err)
	require.Equal(t, mainSHA, got)
}
