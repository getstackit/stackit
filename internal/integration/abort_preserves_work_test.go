package integration

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// gitStatus returns the porcelain status of the test repo, which encodes the
// staged/unstaged/untracked classification of every change.
func (s *TestShell) gitStatus() string {
	s.t.Helper()
	s.Git("status --porcelain")
	return strings.TrimSpace(s.Output())
}

// TestAbortRestoresWorkConsumedByModify locks in the contract that `abort` puts
// the user back where they started.
//
// Modify amends the working tree into a commit before it restacks children, so
// a conflict partway through leaves the user's edit living only inside a commit
// that abort is about to roll away. Rolling refs back is not enough: without
// the working tree coming back with them, the edit survives only in the reflog,
// which is indistinguishable from losing it for anyone who does not think to go
// looking there.
func TestAbortRestoresWorkConsumedByModify(t *testing.T) {
	t.Parallel()
	sh := NewTestShellInProcess(t)

	// main -> a -> b, where b edits the same line a is about to.
	sh.WriteFile("shared.txt", "top\nbottom\n").
		Run("create a -m 'a commit'").
		OnBranch("a")
	sh.WriteFile("shared.txt", "top\nB EDIT\n").
		Run("create b -m 'b commit'").
		OnBranch("b")

	sh.Checkout("a")
	originalA := sh.revParse("a")

	// The edit the user is about to lose: an unstaged change to a tracked file,
	// plus a brand new file they have not staged. Neither exists anywhere but
	// on disk.
	sh.WriteUnstaged("shared.txt", "top\nA EDIT\n").
		WriteUnstaged("notes.txt", "scratch notes\n")
	statusBefore := sh.gitStatus()

	// modify -a stages both, amends them into a's commit, and conflicts
	// restacking b onto the rewritten a.
	sh.RunExpectError("modify -a").
		OutputContains("Conflicted files")

	sh.Run("abort --force").
		OutputContains("Aborting rebase").
		OutputContains("Restored the uncommitted changes")

	// Where we started: same branch, same commit, same working tree.
	sh.OnBranch("a")
	require.Equal(t, originalA, sh.revParse("a"),
		"abort must roll the amended commit back off a")
	require.Equal(t, statusBefore, sh.gitStatus(),
		"abort must restore the working tree, including the staged/unstaged split")
	require.Equal(t, "top\nA EDIT\n", sh.fileContent("shared.txt"),
		"the edit modify consumed must be back in the working tree")
	require.Equal(t, "scratch notes\n", sh.fileContent("notes.txt"),
		"an untracked file modify committed must be back on disk")
}

// TestAbortRestoresStagedWork covers the staged half of the split: work the
// user had staged must come back staged, not merely present.
func TestAbortRestoresStagedWork(t *testing.T) {
	t.Parallel()
	sh := NewTestShellInProcess(t)

	sh.WriteFile("shared.txt", "top\nbottom\n").
		Run("create a -m 'a commit'")
	sh.WriteFile("shared.txt", "top\nB EDIT\n").
		Run("create b -m 'b commit'")

	sh.Checkout("a")
	originalA := sh.revParse("a")

	// Staged edit plus a staged new file, the shape `git add -A` leaves behind.
	sh.WriteFile("shared.txt", "top\nA EDIT\n").
		WriteFile("extra.txt", "extra work\n")
	statusBefore := sh.gitStatus()
	require.Contains(t, statusBefore, "A  extra.txt", "precondition: the new file is staged")

	sh.RunExpectError("modify").
		OutputContains("Conflicted files")
	sh.Run("abort --force")

	require.Equal(t, originalA, sh.revParse("a"))
	require.Equal(t, statusBefore, sh.gitStatus(),
		"staged work must come back staged")
	require.Equal(t, "extra work\n", sh.fileContent("extra.txt"))
}

// TestAbortLeavesCleanWorktreeClean guards the other direction: a command that
// consumed nothing must not have anything handed back to it. A restack starts
// from a clean tree, so aborting one leaves it clean.
func TestAbortLeavesCleanWorktreeClean(t *testing.T) {
	t.Parallel()
	sh := NewTestShellInProcess(t)

	sh.WriteFile("shared.txt", "top\nbottom\n").
		Run("create a -m 'a commit'")
	sh.WriteFile("shared.txt", "top\nB EDIT\n").
		Run("create b -m 'b commit'")

	// Rewrite a behind b's back so the restack of b conflicts, leaving the
	// working tree clean when the restack starts.
	sh.Checkout("a").
		WriteFile("shared.txt", "top\nA EDIT\n").
		Git("commit --amend --no-edit")
	require.Empty(t, sh.gitStatus(), "precondition: the restack starts from a clean tree")
	originalA := sh.revParse("a")

	sh.RunExpectError("restack --upstack").
		OutputContains("conflict")
	sh.Run("abort --force").
		OutputNotContains("Restored the uncommitted changes")

	require.Equal(t, originalA, sh.revParse("a"))
	require.Empty(t, sh.gitStatus(), "abort must not invent working tree changes")
	require.NoFileExists(t, filepath.Join(sh.Dir(), ".git", "rebase-merge"),
		"abort must leave no rebase in progress")
}

// TestUndoRestoresWorkConsumedByModify applies the same contract to undo: it
// restores the repository to before a command, and the working tree that
// command consumed is part of that state.
func TestUndoRestoresWorkConsumedByModify(t *testing.T) {
	t.Parallel()
	sh := NewTestShellInProcess(t)

	sh.WriteFile("shared.txt", "top\nbottom\n").
		Run("create a -m 'a commit'")

	originalA := sh.revParse("a")
	sh.WriteUnstaged("shared.txt", "top\nA EDIT\n")
	statusBefore := sh.gitStatus()

	// No children, so this modify succeeds outright: the work is now inside a's
	// amended commit and nowhere else.
	sh.Run("modify -a")
	require.NotEqual(t, originalA, sh.revParse("a"))
	require.Empty(t, sh.gitStatus())

	sh.Run("undo --snapshot " + sh.GetLatestSnapshotID() + " --yes").
		OutputContains("Restored the uncommitted changes")

	require.Equal(t, originalA, sh.revParse("a"))
	require.Equal(t, statusBefore, sh.gitStatus())
	require.Equal(t, "top\nA EDIT\n", sh.fileContent("shared.txt"))
}

// TestUndoRefusesToOverwriteUncommittedWork covers the other direction: undo
// resets the working tree onto the restored commits, so running it with edits
// of your own in the tree would destroy them — and then merge the snapshot's
// capture into the wreckage. Refusing is the only outcome that keeps the work.
//
// Untracked files are deliberately not a refusal: a reset cannot destroy them,
// and the untracked half of a capture never overwrites a file already on disk.
func TestUndoRefusesToOverwriteUncommittedWork(t *testing.T) {
	t.Parallel()
	sh := NewTestShellInProcess(t)

	sh.WriteFile("shared.txt", "top\nbottom\n").
		Run("create a -m 'a commit'").
		OnBranch("a")

	// A snapshot to undo to, and a commit for undo to roll back.
	sh.WriteFile("shared.txt", "top\namended\n").
		Run("modify -m 'amended a'")
	amendedA := sh.revParse("a")

	// An untracked file alone must not block undo.
	sh.WriteUnstaged("scratch.txt", "untracked\n").
		Run("undo -y")
	require.NotEqual(t, amendedA, sh.revParse("a"),
		"an untracked file must not stop undo from rolling the amend back")
	require.Equal(t, "untracked\n", sh.fileContent("scratch.txt"),
		"undo must leave untracked files alone")

	// A tracked edit must.
	sh.WriteUnstaged("shared.txt", "top\nMY OWN WORK\n")
	beforeA := sh.revParse("a")

	sh.RunExpectError("undo -y")

	require.Equal(t, beforeA, sh.revParse("a"),
		"a refused undo must not move any ref")
	require.Equal(t, "top\nMY OWN WORK\n", sh.fileContent("shared.txt"),
		"a refused undo must leave the working tree untouched")
}
