package git_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/testhelpers"
)

func TestBatchCommitInfo(t *testing.T) {
	t.Parallel()
	scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)

	err := scene.Repo.CreateAndCheckoutBranch("branch1")
	require.NoError(t, err)
	err = scene.Repo.CreateChangeAndCommit("branch1 change", "b1")
	require.NoError(t, err)

	runner := git.NewRunnerWithPath(scene.Dir, nil)

	wantMainDate, err := runner.GetCommitDate("main")
	require.NoError(t, err)
	wantMainAuthor, err := runner.GetCommitAuthor("main")
	require.NoError(t, err)
	wantBranch1Date, err := runner.GetCommitDate("branch1")
	require.NoError(t, err)
	wantBranch1Author, err := runner.GetCommitAuthor("branch1")
	require.NoError(t, err)

	got := runner.BatchCommitInfo([]string{"main", "branch1", "no-such-branch"})

	require.Len(t, got, 2, "unmatched branch should be omitted, not errored")
	require.True(t, wantMainDate.Equal(got["main"].Date))
	require.Equal(t, wantMainAuthor, got["main"].Author)
	require.True(t, wantBranch1Date.Equal(got["branch1"].Date))
	require.Equal(t, wantBranch1Author, got["branch1"].Author)
}

func TestBatchCommitInfo_Empty(t *testing.T) {
	t.Parallel()
	scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)

	runner := git.NewRunnerWithPath(scene.Dir, nil)
	got := runner.BatchCommitInfo(nil)
	require.Empty(t, got)
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
	got, err := runner.GetRemoteRevision("main")
	require.NoError(t, err)
	require.Equal(t, mainSHA, got)
}
