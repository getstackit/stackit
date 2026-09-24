package git_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/testhelpers"
)

func TestStashCaptureIntentToAddPreservesIndex(t *testing.T) {
	t.Parallel()
	scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)
	runner := git.NewRunnerWithPath(scene.Dir, nil)
	run := func(args ...string) string {
		t.Helper()
		out, err := runner.RunGitCommandRawWithContext(t.Context(), args...)
		require.NoError(t, err)
		return out
	}
	write := func(path, content string) {
		t.Helper()
		require.NoError(t, os.WriteFile(filepath.Join(scene.Dir, path), []byte(content), 0o600))
	}
	write("tracked.txt", "base\n")
	write("rename-source.txt", "rename content\n")
	run("add", "tracked.txt", "rename-source.txt")
	run("commit", "-m", "base")
	write("tracked.txt", "staged\n")
	run("add", "tracked.txt")
	write("tracked.txt", "unstaged\n")
	require.NoError(t, os.Remove(filepath.Join(scene.Dir, "rename-source.txt")))
	files := map[string]string{"new.txt": "new work\n", "empty.txt": "", ":(glob)*.txt": "literal\n", "line\nbreak.txt": "newline\n", "rename-target.txt": "rename content\n"}
	for path, content := range files {
		write(path, content)
		run("--literal-pathspecs", "add", "-N", "--", path)
	}
	before := run("status", "--porcelain", "-z")
	staged := run("diff", "--cached", "--binary")
	stashList := run("stash", "list")
	capture, err := runner.StashCreate(t.Context(), "capture")
	require.NoError(t, err)
	require.NotEmpty(t, capture.SHA)
	require.Len(t, capture.IntentToAddPaths, len(files))
	require.Equal(t, before, run("status", "--porcelain", "-z"))
	require.Equal(t, staged, run("diff", "--cached", "--binary"))
	require.Equal(t, stashList, run("stash", "list"))
	run("reset", "--hard", "HEAD")
	require.NoError(t, runner.StashApplyRef(t.Context(), capture.SHA, git.StashApplyWithIndex))
	require.NoError(t, runner.RestoreIntentToAdd(t.Context(), capture.IntentToAddPaths))
	require.Equal(t, before, run("status", "--porcelain", "-z"))
	require.Equal(t, staged, run("diff", "--cached", "--binary"))
	for path, content := range files {
		got, err := os.ReadFile(filepath.Join(scene.Dir, path))
		require.NoError(t, err)
		require.Equal(t, content, string(got))
	}
}
