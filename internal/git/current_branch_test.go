package git

import (
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// GetCurrentBranch reads HEAD off disk instead of spawning `git symbolic-ref`,
// so these cases pin that the fast path agrees with git and that the states it
// deliberately does not handle still fall through to git.
func TestGetCurrentBranch(t *testing.T) {
	t.Parallel()

	run := func(t *testing.T, dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, out)
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

	t.Run("on a branch", func(t *testing.T) {
		t.Parallel()
		dir := newRepo(t)
		run(t, dir, "checkout", "-b", "feature/slash-name")

		branch, err := NewRunnerWithPath(dir, nil).GetCurrentBranch()
		require.NoError(t, err)
		require.Equal(t, "feature/slash-name", branch)
	})

	t.Run("detached HEAD falls back to git and errors", func(t *testing.T) {
		t.Parallel()
		dir := newRepo(t)
		run(t, dir, "checkout", "--detach")

		_, err := NewRunnerWithPath(dir, nil).GetCurrentBranch()
		require.ErrorContains(t, err, "HEAD is not on a branch")
	})

	// A linked worktree keeps its own HEAD under .git/worktrees/<name>, so
	// reading the main repo's HEAD here would report the wrong branch.
	t.Run("linked worktree reports its own branch", func(t *testing.T) {
		t.Parallel()
		dir := newRepo(t)
		wt := filepath.Join(t.TempDir(), "wt")
		run(t, dir, "worktree", "add", "-b", "wt-branch", wt)

		branch, err := NewRunnerWithPath(wt, nil).GetCurrentBranch()
		require.NoError(t, err)
		require.Equal(t, "wt-branch", branch)
	})
}
