package git_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/testhelpers"
)

func TestReadWorktreeStatus(t *testing.T) {
	t.Parallel()
	scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)
	logger := &traceCaptureLogger{}
	runner := git.NewRunnerWithPath(scene.Dir, logger)
	ctx := t.Context()
	write := func(name, content string) {
		t.Helper()
		require.NoError(t, os.WriteFile(filepath.Join(scene.Dir, name), []byte(content), 0600))
	}
	command := func(args ...string) {
		t.Helper()
		require.NoError(t, scene.Repo.RunGitCommand(args...))
	}
	check := func(want git.WorktreeStatus) {
		t.Helper()
		logger.calls = 0
		got, err := runner.ReadWorktreeStatus(ctx)
		require.NoError(t, err)
		require.Equal(t, want, got)
		require.Equal(t, 1, logger.calls)
	}
	check(git.WorktreeStatus{})
	write("old\nname\t.txt", "old\n")
	command("add", "-A")
	command("commit", "-m", "file")
	command("mv", "old\nname\t.txt", "renamed\nname.txt")
	check(git.WorktreeStatus{Staged: true})
	write("renamed\nname.txt", "changed\n")
	write("untracked\nfile", "new\n")
	command("config", "status.showUntrackedFiles", "no")
	check(git.WorktreeStatus{Staged: true, Unstaged: true, Untracked: true})
	command("add", "-A")
	check(git.WorktreeStatus{Staged: true})
	command("commit", "-m", "changes")
	check(git.WorktreeStatus{})
	write("intent-to-add", "new\n")
	command("add", "-N", "intent-to-add")
	check(git.WorktreeStatus{Unstaged: true})
}
