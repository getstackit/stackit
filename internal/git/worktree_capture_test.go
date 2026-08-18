package git_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/testhelpers"
)

// TestCaptureUntracked_RoundTripsNestedAndAwkwardPaths covers the paths a
// stash cannot hold. Untracked files are the only working-tree content with no
// copy anywhere else, so a capture that drops a nested or space-containing path
// loses that file outright when a rollback deletes it.
func TestCaptureUntracked_RoundTripsNestedAndAwkwardPaths(t *testing.T) {
	t.Parallel()
	scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)
	runner := git.NewRunnerWithPath(scene.Dir, nil)
	ctx := context.Background()

	require.NoError(t, os.MkdirAll(filepath.Join(scene.Dir, "nested", "deep"), 0o750))
	files := map[string]string{
		"top.txt":                     "top level\n",
		"nested/deep/inner.txt":       "nested\n",
		"nested/name with spaces.txt": "spaced\n",
	}
	for name, content := range files {
		require.NoError(t, os.WriteFile(filepath.Join(scene.Dir, name), []byte(content), 0o600))
	}

	sha, err := runner.CaptureUntracked(ctx, "capture")
	require.NoError(t, err)
	require.NotEmpty(t, sha)

	// The capture must not have touched the working tree or the index.
	status, err := runner.RunGitCommandWithContext(ctx, "status", "--porcelain")
	require.NoError(t, err)
	require.Contains(t, status, "?? top.txt", "captured files must still be untracked")

	for name := range files {
		require.NoError(t, os.Remove(filepath.Join(scene.Dir, name)))
	}

	count, err := runner.RestoreUntracked(ctx, sha)
	require.NoError(t, err)
	require.Equal(t, len(files), count)

	for name, content := range files {
		restored, readErr := os.ReadFile(filepath.Join(scene.Dir, name))
		require.NoError(t, readErr, "%s must be restored", name)
		require.Equal(t, content, string(restored))
	}

	// Restored files stay untracked: the index was never part of the capture.
	status, err = runner.RunGitCommandWithContext(ctx, "status", "--porcelain")
	require.NoError(t, err)
	require.Contains(t, status, "?? top.txt")
}

// TestRestoreUntracked_LeavesExistingFilesAlone locks in the rule that a
// safety net never overwrites content that is currently on disk.
func TestRestoreUntracked_LeavesExistingFilesAlone(t *testing.T) {
	t.Parallel()
	scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)
	runner := git.NewRunnerWithPath(scene.Dir, nil)
	ctx := context.Background()

	path := filepath.Join(scene.Dir, "scratch.txt")
	require.NoError(t, os.WriteFile(path, []byte("captured\n"), 0o600))

	sha, err := runner.CaptureUntracked(ctx, "capture")
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(path, []byte("newer\n"), 0o600))

	count, err := runner.RestoreUntracked(ctx, sha)
	require.NoError(t, err)
	require.Zero(t, count, "an existing file must not be restored over")

	content, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "newer\n", string(content))
}

// TestCaptureUntracked_CleanTree reports nothing to capture rather than an
// empty commit, so snapshots of a clean repository stay ref-only.
func TestCaptureUntracked_CleanTree(t *testing.T) {
	t.Parallel()
	scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)
	runner := git.NewRunnerWithPath(scene.Dir, nil)

	sha, err := runner.CaptureUntracked(context.Background(), "capture")
	require.NoError(t, err)
	require.Empty(t, sha)
}

// TestCaptureUntracked_SkipsIgnoredFiles keeps build output out of the
// capture. A snapshot is taken on every modify/create/absorb/split, so
// sweeping in node_modules or a bin/ directory would make each one enormous —
// and ignored files are not work the user can lose to a rollback anyway.
func TestCaptureUntracked_SkipsIgnoredFiles(t *testing.T) {
	t.Parallel()
	scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)
	runner := git.NewRunnerWithPath(scene.Dir, nil)
	ctx := context.Background()

	require.NoError(t, os.WriteFile(filepath.Join(scene.Dir, ".gitignore"), []byte("build/\n"), 0o600))
	require.NoError(t, os.MkdirAll(filepath.Join(scene.Dir, "build"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(scene.Dir, "build", "artifact.bin"), []byte("junk\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(scene.Dir, "keep.txt"), []byte("real work\n"), 0o600))

	sha, err := runner.CaptureUntracked(ctx, "capture")
	require.NoError(t, err)
	require.NotEmpty(t, sha)

	captured, err := runner.RunGitCommandWithContext(ctx, "ls-tree", "-r", "--name-only", sha)
	require.NoError(t, err)
	require.Contains(t, captured, "keep.txt")
	require.Contains(t, captured, ".gitignore", "an untracked .gitignore is itself work to preserve")
	require.NotContains(t, captured, "build/artifact.bin", "ignored files must stay out of the capture")
}
