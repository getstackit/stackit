package engine_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/testhelpers"
)

// ResetTrunkToRemote is the one destructive path that discards local trunk
// commits outright. It no longer checks trunk out to do it — it moves the ref
// with a compare-and-swap and then synchronizes the clean worktree holding it —
// so the refusals that keep it from resetting over uncommitted work are all
// that stands between a `sync --force` and lost work.
func TestResetTrunkToRemote(t *testing.T) {
	t.Parallel()

	newEngine := func(t *testing.T) (*testhelpers.Scene, engine.Engine) {
		t.Helper()
		scene := testhelpers.NewRemoteSceneParallel(t)
		eng, err := engine.NewEngine(engine.Options{
			RepoRoot: scene.Dir,
			Trunk:    "main",
			Git:      git.NewRunnerWithPath(scene.Dir, nil),
		})
		require.NoError(t, err)
		return scene, eng
	}

	t.Run("discards local trunk commits the remote does not have", func(t *testing.T) {
		t.Parallel()
		scene, eng := newEngine(t)

		remoteSHA, err := scene.Repo.GetRevision("origin/main")
		require.NoError(t, err)

		// Local trunk moves ahead of the remote — exactly the state
		// `sync --force` exists to throw away.
		require.NoError(t, scene.Repo.CreateChangeAndCommit("local only", "local-only"))
		aheadSHA, err := scene.Repo.GetRevision("main")
		require.NoError(t, err)
		require.NotEqual(t, remoteSHA, aheadSHA)

		require.NoError(t, eng.ResetTrunkToRemote(context.Background()))

		got, err := scene.Repo.GetRevision("main")
		require.NoError(t, err)
		require.Equal(t, remoteSHA, got, "trunk should sit on the remote revision")

		// The worktree came with the ref rather than being left on the old
		// content under a new one — that divergence is what a later
		// `modify -a` commits as a deletion.
		status, err := scene.Repo.RunGitCommandAndGetOutput("status", "--porcelain")
		require.NoError(t, err)
		require.Empty(t, status, "worktree should match the reset ref")
	})

	t.Run("refuses when the trunk worktree has uncommitted changes", func(t *testing.T) {
		t.Parallel()
		scene, eng := newEngine(t)

		require.NoError(t, scene.Repo.CreateChangeAndCommit("local only", "local-only"))
		before, err := scene.Repo.GetRevision("main")
		require.NoError(t, err)

		// Uncommitted tracked work in the worktree holding trunk. Resetting
		// over it would discard it with no way back.
		tracked := filepath.Join(scene.Dir, "local-only_test.txt")
		require.FileExists(t, tracked, "CreateChangeAndCommit should have committed this path")
		require.NoError(t, os.WriteFile(tracked, []byte("work in progress"), 0o600))

		err = eng.ResetTrunkToRemote(context.Background())
		require.Error(t, err)
		require.Contains(t, err.Error(), "refusing to move main")
		require.Contains(t, err.Error(), "uncommitted changes")

		after, revErr := scene.Repo.GetRevision("main")
		require.NoError(t, revErr)
		require.Equal(t, before, after, "a refusal must leave the trunk ref untouched")

		content, readErr := os.ReadFile(tracked)
		require.NoError(t, readErr)
		require.Equal(t, "work in progress", string(content), "the in-progress edit must survive")
	})
}
