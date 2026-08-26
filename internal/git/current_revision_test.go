package git

import (
	"context"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/require"
)

// GetCurrentRevision reads HEAD off disk instead of spawning `git rev-parse
// --verify HEAD` when HEAD is detached, so these cases pin that the fast
// path agrees with git and that the states it deliberately does not handle
// still fall through to git.
func TestGetCurrentRevision(t *testing.T) {
	t.Parallel()

	run := func(t *testing.T, dir string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, out)
		return string(out)
	}

	newRepo := func(t *testing.T) string {
		t.Helper()
		dir := t.TempDir()
		run(t, dir, "init", "-b", "main")
		run(t, dir, "config", "user.email", "t@example.com")
		run(t, dir, "config", "user.name", "t")
		run(t, dir, "commit", "--allow-empty", "-m", "init")
		return dir
	}

	revParse := func(t *testing.T, dir string) string {
		t.Helper()
		out := run(t, dir, "rev-parse", "HEAD")
		return out[:len(out)-1] // trim trailing newline
	}

	t.Run("detached HEAD", func(t *testing.T) {
		t.Parallel()
		dir := newRepo(t)
		run(t, dir, "checkout", "--detach")

		rev, err := NewRunnerWithPath(dir, nil).GetCurrentRevision(context.Background())
		require.NoError(t, err)
		require.Equal(t, revParse(t, dir), rev)
	})

	t.Run("on a branch falls back to git", func(t *testing.T) {
		t.Parallel()
		dir := newRepo(t)
		run(t, dir, "checkout", "-b", "feature")

		rev, err := NewRunnerWithPath(dir, nil).GetCurrentRevision(context.Background())
		require.NoError(t, err)
		require.Equal(t, revParse(t, dir), rev)
	})
}
