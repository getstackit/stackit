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

// TestHardReset_HonorsCanceledContext locks in the cancellation behavior that
// the go-git-based implementation lacked: worktree.Reset() took no context, so
// callers passing a canceled or deadline-exceeded ctx could not interrupt a
// long-running reset.
func TestHardReset_HonorsCanceledContext(t *testing.T) {
	t.Parallel()
	scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)
	runner := git.NewRunnerWithPath(scene.Dir, nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := runner.HardReset(ctx, "HEAD")
	require.Error(t, err)
}

// TestHardReset_DiscardsUncommittedChanges is a smoke test that the native
// `git reset --hard` path preserves the behavior callers depend on.
func TestHardReset_DiscardsUncommittedChanges(t *testing.T) {
	t.Parallel()
	scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)
	runner := git.NewRunnerWithPath(scene.Dir, nil)

	tracked := filepath.Join(scene.Dir, "init_test.txt")
	original, err := os.ReadFile(tracked)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(tracked, []byte("dirty"), 0o600))

	require.NoError(t, runner.HardReset(context.Background(), "HEAD"))

	after, err := os.ReadFile(tracked)
	require.NoError(t, err)
	require.Equal(t, original, after)
}

// TestSoftReset_PreservesWorkingTree verifies the soft mode does not touch
// the index or working tree, matching the previous go-git semantics.
func TestSoftReset_PreservesWorkingTree(t *testing.T) {
	t.Parallel()
	scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)
	runner := git.NewRunnerWithPath(scene.Dir, nil)

	require.NoError(t, scene.Repo.CreateChangeAndCommit("second", "second"))

	tracked := filepath.Join(scene.Dir, "second_test.txt")
	before, err := os.ReadFile(tracked)
	require.NoError(t, err)

	require.NoError(t, runner.SoftReset(context.Background(), "HEAD~1"))

	after, err := os.ReadFile(tracked)
	require.NoError(t, err)
	require.Equal(t, before, after)
}
