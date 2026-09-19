package git_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/testhelpers"
)

func TestReadRevisionsMissingRefsStayBatched(t *testing.T) {
	t.Parallel()
	scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)
	logger := &traceCaptureLogger{}
	runner := git.NewRunnerWithPath(scene.Dir, logger)
	main, err := runner.ReadRevisions(context.Background(), "main").One()
	require.NoError(t, err)
	names := make([]string, 1, 33)
	names[0] = "main"
	for i := range 30 {
		names = append(names, fmt.Sprintf("origin/unpublished-%d", i))
	}
	names = append(names, "HEAD", "main")
	logger.calls = 0
	got, errs := runner.ReadRevisions(context.Background(), names...).ValuesAndErrors()
	require.Equal(t, map[string]string{"main": main, "HEAD": main}, got)
	require.Len(t, errs, 30)
	for _, err := range errs {
		require.ErrorContains(t, err, "reference not found")
	}
	require.Equal(t, 1, logger.calls, "one batch, regardless of missing-ref count")
}

func TestReadRevisionsFallbackPreservesResolution(t *testing.T) {
	t.Parallel()
	scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)
	runner := git.NewRunnerWithPath(scene.Dir, nil)
	require.NoError(t, scene.Repo.CreateChangeAndCommit("second commit", "second"))
	sha, err := runner.ReadRevisions(context.Background(), "HEAD").One()
	require.NoError(t, err)
	blob, err := git.One(runner.CreateBlobs(context.Background(), "metadata"))
	require.NoError(t, err)
	require.NoError(t, runner.UpdateRefs(context.Background(), []git.RefUpdate{{RefName: "refs/stackit/metadata/main", NewSHA: blob}}, ""))
	require.NoError(t, scene.Repo.RunGitCommand("tag", "-a", "commit-tag", "-m", "commit tag"))
	require.NoError(t, scene.Repo.RunGitCommand("tag", "-a", "blob-tag", blob, "-m", "blob tag"))
	require.NoError(t, scene.Repo.RunGitCommand("branch", "origin/local", sha))
	require.NoError(t, runner.UpdateRefs(context.Background(), []git.RefUpdate{{RefName: "refs/remotes/upstream/main", NewSHA: sha}}, ""))

	names := []string{"HEAD", "HEAD~1", sha[:12], "commit-tag", "blob-tag",
		"HEAD^{tree}", "refs/stackit/metadata/main", "origin/local", "upstream/main",
		strings.Repeat("A", 40)}
	want := make(map[string]string)
	for _, name := range names {
		want[name], err = runner.ReadRevisions(context.Background(), name).One()
		require.NoError(t, err)
	}
	// A failure at any position must not lose valid refs before or after it.
	for _, position := range []int{0, len(names) / 2, len(names)} {
		input := append([]string{}, names[:position]...)
		input = append(input, "missing-ref")
		input = append(input, names[position:]...)
		got, errs := runner.ReadRevisions(context.Background(), input...).ValuesAndErrors()
		require.Equal(t, want, got)
		require.Len(t, errs, 1)
		require.ErrorContains(t, errs[0], "missing-ref")
	}

	// One and many inputs both peel annotated commit tags.
	tagSHA, err := runner.RunGitCommandWithContext(t.Context(), "rev-parse", "commit-tag")
	require.NoError(t, err)
	got, errs := runner.ReadRevisions(context.Background(), []string{"commit-tag"}...).ValuesAndErrors()
	require.Empty(t, errs)
	require.Equal(t, sha, got["commit-tag"])
	require.NotEqual(t, sha, tagSHA)
}

func TestReadRevisionsNewlineExpressionFallback(t *testing.T) {
	t.Parallel()
	scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)
	require.NoError(t, os.WriteFile(filepath.Join(scene.Dir, "with\nnewline"), []byte("content"), 0o600))
	require.NoError(t, scene.Repo.RunGitCommand("add", "."))
	require.NoError(t, scene.Repo.RunGitCommand("commit", "-m", "newline path"))
	runner := git.NewRunnerWithPath(scene.Dir, nil)
	name := "HEAD:with\nnewline"
	want, err := runner.ReadRevisions(context.Background(), name).One()
	require.NoError(t, err)
	got, errs := runner.ReadRevisions(context.Background(), []string{"missing-ref", name}...).ValuesAndErrors()
	require.Equal(t, map[string]string{name: want}, got)
	require.Len(t, errs, 1)
}
