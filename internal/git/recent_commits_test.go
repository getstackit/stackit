package git_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/testhelpers"
)

func TestGetRecentCommits_DuplicateTrailers(t *testing.T) {
	t.Parallel()
	scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)

	err := scene.Repo.CreateChange("change", "dup", false)
	require.NoError(t, err)
	err = scene.Repo.RunGitCommand("add", ".")
	require.NoError(t, err)
	err = scene.Repo.RunGitCommand(
		"commit",
		"-m", "Consolidate stack [PROJ-123]",
		"-m", "Stackit-Stack-Size: 2\nStackit-Stack-Size: 2\nStackit-PRs: 1,2\nStackit-PRs: 1,2\nStackit-Scope: PROJ-123\nStackit-Scope: PROJ-123",
	)
	require.NoError(t, err)

	runner := git.NewRunnerWithPath(scene.Dir, nil)
	commits, err := runner.GetRecentCommits("main", 1)
	require.NoError(t, err)
	require.Len(t, commits, 1)

	require.Equal(t, 2, commits[0].StackSize)
	require.Equal(t, []int{1, 2}, commits[0].StackPRNumbers)
	require.Equal(t, "PROJ-123", commits[0].StackScope)
	require.Equal(t, git.RecentCommitKindStackMerge, commits[0].Kind)
}

func TestGetRecentCommits_WithoutTrailers(t *testing.T) {
	t.Parallel()
	scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)

	err := scene.Repo.CreateChange("file1", "content1", false)
	require.NoError(t, err)
	err = scene.Repo.RunGitCommand("add", ".")
	require.NoError(t, err)
	err = scene.Repo.RunGitCommand("commit", "-m", "Regular commit without trailers")
	require.NoError(t, err)

	runner := git.NewRunnerWithPath(scene.Dir, nil)
	commits, err := runner.GetRecentCommits("main", 1)
	require.NoError(t, err)
	require.Len(t, commits, 1)

	require.Equal(t, "Regular commit without trailers", commits[0].Subject)
	require.Equal(t, 0, commits[0].StackSize)
	require.Empty(t, commits[0].StackPRNumbers)
	require.Equal(t, "", commits[0].StackScope)
	require.Equal(t, git.RecentCommitKindRegular, commits[0].Kind)
}

func TestGetRecentCommits_MultipleCommits(t *testing.T) {
	t.Parallel()
	scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)

	// Create a regular commit
	err := scene.Repo.CreateChange("file1", "content1", false)
	require.NoError(t, err)
	err = scene.Repo.RunGitCommand("add", ".")
	require.NoError(t, err)
	err = scene.Repo.RunGitCommand("commit", "-m", "First commit")
	require.NoError(t, err)

	// Create a commit with trailers
	err = scene.Repo.CreateChange("file2", "content2", false)
	require.NoError(t, err)
	err = scene.Repo.RunGitCommand("add", ".")
	require.NoError(t, err)
	err = scene.Repo.RunGitCommand(
		"commit",
		"-m", "Consolidate stack",
		"-m", "Stackit-Stack-Size: 3\nStackit-PRs: 10,20,30\nStackit-Scope: FEAT-1",
	)
	require.NoError(t, err)

	runner := git.NewRunnerWithPath(scene.Dir, nil)
	commits, err := runner.GetRecentCommits("main", 3)
	require.NoError(t, err)
	require.Len(t, commits, 3)

	// Most recent first (commit with trailers)
	require.Equal(t, "Consolidate stack", commits[0].Subject)
	require.Equal(t, 3, commits[0].StackSize)
	require.Equal(t, []int{10, 20, 30}, commits[0].StackPRNumbers)
	require.Equal(t, "FEAT-1", commits[0].StackScope)
	require.Equal(t, git.RecentCommitKindStackMerge, commits[0].Kind)

	// Second commit (no trailers)
	require.Equal(t, "First commit", commits[1].Subject)
	require.Equal(t, 0, commits[1].StackSize)
	require.Empty(t, commits[1].StackPRNumbers)
	require.Equal(t, git.RecentCommitKindRegular, commits[1].Kind)

	// Initial commit
	require.Equal(t, "initial", commits[2].Subject)
}

func TestGetRecentCommits_ParsesPRNumberSuffix(t *testing.T) {
	t.Parallel()
	scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)

	err := scene.Repo.CreateChange("file1", "content1", false)
	require.NoError(t, err)
	err = scene.Repo.RunGitCommand("add", ".")
	require.NoError(t, err)
	err = scene.Repo.RunGitCommand("commit", "-m", "Add status badge (#123)")
	require.NoError(t, err)

	runner := git.NewRunnerWithPath(scene.Dir, nil)
	commits, err := runner.GetRecentCommits("main", 1)
	require.NoError(t, err)
	require.Len(t, commits, 1)
	require.Equal(t, 123, commits[0].PRNumber)
	require.Equal(t, git.RecentCommitKindRegular, commits[0].Kind)
}

func TestGetRecentCommits_MergeCommitSkipsTrailerSubject(t *testing.T) {
	t.Parallel()
	scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)

	// Simulate what GitHub creates when merging a consolidation PR:
	// Subject: "Merge pull request #786 from org/stack-merge-..."
	// Body starts with trailers, no descriptive first line
	err := scene.Repo.CreateChange("file1", "content1", false)
	require.NoError(t, err)
	err = scene.Repo.RunGitCommand("add", ".")
	require.NoError(t, err)
	err = scene.Repo.RunGitCommand(
		"commit",
		"-m", "Merge pull request #786 from org/stack-merge-stack-123",
		"-m", "Stackit-Stack-Size: 11\nStackit-PRs: 1,2,3\nStackit-Scope: PROJ-1",
	)
	require.NoError(t, err)

	runner := git.NewRunnerWithPath(scene.Dir, nil)
	commits, err := runner.GetRecentCommits("main", 1)
	require.NoError(t, err)
	require.Len(t, commits, 1)

	// Should fall back to original merge subject, not "Stackit-Stack-Size: 11"
	require.Equal(t, "Merge pull request #786 from org/stack-merge-stack-123", commits[0].Subject)
	require.Equal(t, 786, commits[0].PRNumber)
	require.Equal(t, 11, commits[0].StackSize)
}

func TestGetRecentCommits_MergeCommitWithTitleBeforeTrailers(t *testing.T) {
	t.Parallel()
	scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)

	// When the body has a descriptive line before trailers
	err := scene.Repo.CreateChange("file1", "content1", false)
	require.NoError(t, err)
	err = scene.Repo.RunGitCommand("add", ".")
	require.NoError(t, err)
	err = scene.Repo.RunGitCommand(
		"commit",
		"-m", "Merge pull request #786 from org/stack-merge-stack-123",
		"-m", "Consolidate auth stack\n\nStackit-Stack-Size: 3\nStackit-PRs: 1,2,3",
	)
	require.NoError(t, err)

	runner := git.NewRunnerWithPath(scene.Dir, nil)
	commits, err := runner.GetRecentCommits("main", 1)
	require.NoError(t, err)
	require.Len(t, commits, 1)

	// Should use the descriptive line, not the trailer
	require.Equal(t, "Consolidate auth stack", commits[0].Subject)
	require.Equal(t, 786, commits[0].PRNumber)
}
