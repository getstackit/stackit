package integration

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// buildConflictingTrio sets up main -> a -> b -> c where b and c touch the same
// line, so replaying c over a different b conflicts. Each branch comes from
// `stackit create`, so the newest snapshot on disk belongs to `create c` and
// was taken before c existed — restoring it deletes c.
func buildConflictingTrio(t *testing.T) *TestShell {
	t.Helper()
	sh := NewTestShellInProcess(t)

	sh.WriteFile("shared.txt", "l1\nl2-A\nl3\n").
		Run("create a -m 'feat: a'")
	sh.WriteFile("shared.txt", "l1\nl2-A\nl3-B\n").
		Run("create b -m 'feat: b'")
	sh.WriteFile("shared.txt", "l1\nl2-A\nl3-B-C\n").
		Run("create c -m 'feat: c'")

	sh.HasBranches("a", "b", "c", "main")
	return sh
}

// gitAllowError runs a raw git command that is expected to fail (a rebase that
// conflicts), leaving the repository in the halted state under test.
func (s *TestShell) gitAllowError(args ...string) {
	s.t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = s.scene.Dir
	output, _ := cmd.CombinedOutput()
	s.lastOutput = string(output)
}

// writeRepoFile writes a file inside the test repo by path, including paths
// under .git that no stackit command would create.
func (s *TestShell) writeRepoFile(relPath, content string) {
	s.t.Helper()
	path := filepath.Join(s.scene.Dir, relPath)
	require.NoError(s.t, os.WriteFile(path, []byte(content), 0o600))
}

// TestAbortAfterDeleteConflictKeepsUnrelatedBranches locks in the rule that
// abort rolls back the halted command and nothing else.
//
// Deleting a middle branch reparents its child, which conflicts here. Abort
// used to restore whichever snapshot was newest on disk — `create c`'s, taken
// before c existed — so aborting a delete of `b` deleted `c` instead: the
// opposite of what the user asked for, on a branch the delete never rewrote.
func TestAbortAfterDeleteConflictKeepsUnrelatedBranches(t *testing.T) {
	t.Parallel()
	sh := buildConflictingTrio(t)

	bOriginal := sh.revParse("b")
	cOriginal := sh.revParse("c")

	sh.Checkout("main").
		RunExpectError("delete b --force").
		OutputContains("conflict")

	sh.Run("abort --force")

	sh.HasBranches("a", "b", "c", "main")
	require.Equal(t, cOriginal, sh.revParse("c"),
		"aborting a delete must not touch a branch the delete never rewrote")
	require.Equal(t, bOriginal, sh.revParse("b"),
		"the branch the delete was rolling back must come back intact")
}

// TestAbortAfterReorderConflictRestoresOrder covers the same guarantee for
// reorder, which also recorded no rollback point: aborting it deleted the most
// recently created branch instead of undoing the reorder.
//
// Not parallel: reorder's only non-TTY input is an editor chosen through
// GIT_EDITOR, and environment variables are process-global.
func TestAbortAfterReorderConflictRestoresOrder(t *testing.T) {
	sh := buildConflictingTrio(t)

	bOriginal := sh.revParse("b")
	cOriginal := sh.revParse("c")

	// An editor that swaps the last two branches, so c replays before b and
	// conflicts on the line they both touch.
	editor := filepath.Join(t.TempDir(), "swap-editor.sh")
	require.NoError(t, os.WriteFile(editor, []byte(`#!/bin/sh
grep '^#' "$1" > "$1.new"
BRANCHES=$(grep -v '^#' "$1" | grep -v '^$')
printf '%s\n%s\n%s\n' "$(echo "$BRANCHES" | sed -n 1p)" "$(echo "$BRANCHES" | sed -n 3p)" "$(echo "$BRANCHES" | sed -n 2p)" >> "$1.new"
mv "$1.new" "$1"
`), 0o700))
	t.Setenv("GIT_EDITOR", editor)

	sh.Checkout("c").
		RunExpectError("reorder").
		OutputContains("conflict")

	sh.Run("abort --force")

	sh.HasBranches("a", "b", "c", "main")
	require.Equal(t, bOriginal, sh.revParse("b"))
	require.Equal(t, cOriginal, sh.revParse("c"))
	sh.ExpectStackStructure(map[string]string{"a": "main", "b": "a", "c": "b"})
}

// TestAbortWithoutRollbackPointRestoresNothing is the backstop for any command
// that halts without recording a snapshot, including ones that do not exist
// yet. Abort must leave the repository as the command left it and say so,
// rather than reaching for an unrelated snapshot.
func TestAbortWithoutRollbackPointRestoresNothing(t *testing.T) {
	t.Parallel()
	sh := buildConflictingTrio(t)

	// Rewrite a behind b's back, then hand-roll the halted state a
	// snapshot-less command leaves: a real conflicted rebase plus continuation
	// state naming no snapshot.
	sh.Checkout("a").
		WriteFile("shared.txt", "l1\nl2-REWRITTEN\nl3\n").
		Git("commit --amend --no-edit")

	aRewritten := sh.revParse("a")
	bBefore := sh.revParse("b")
	cBefore := sh.revParse("c")

	sh.writeRepoFile(filepath.Join(".git", ".stackit_continue"), `{"currentBranchOverride":"b"}`)
	sh.Git("checkout --detach b")
	sh.gitAllowError("rebase", "--onto", "a", "b~1", "b")

	sh.Run("abort --force").
		OutputContains("no rollback point")

	sh.HasBranches("a", "b", "c", "main")
	require.Equal(t, aRewritten, sh.revParse("a"))
	require.Equal(t, bBefore, sh.revParse("b"))
	require.Equal(t, cBefore, sh.revParse("c"))
}

// TestAbortPrefersAbsorbCleanupOverStaleContinuation covers a continuation file
// left behind by a conflict the user finished with plain `git rebase
// --continue`. A later failed absorb must still get absorb's cleanup: that file
// used to take over abort, skip restoring absorb's stashed changes, and roll
// back to the finished command's snapshot, deleting c.
func TestAbortPrefersAbsorbCleanupOverStaleContinuation(t *testing.T) {
	t.Parallel()
	sh := buildConflictingTrio(t)

	undoDir := filepath.Join(sh.scene.Dir, ".git", "stackit", "undo")
	snapshots, err := filepath.Glob(filepath.Join(undoDir, "*_create.json"))
	require.NoError(t, err)
	require.NotEmpty(t, snapshots)
	createC := strings.TrimSuffix(filepath.Base(snapshots[len(snapshots)-1]), ".json")
	// c has moved on from the tip the conflicted rebase started at: the user
	// finished that rebase by hand.
	sh.writeRepoFile(filepath.Join(".git", ".stackit_continue"),
		`{"currentBranchOverride":"c","expectedBranchRevision":"`+sh.revParse("b")+`","snapshotId":"`+createC+`"}`)

	// Absorb's failure mode: the user's uncommitted work stashed under its marker.
	sh.WriteFile("notes.txt", "uncommitted work\n").
		Git("stash push --include-untracked -m stackit-absorb-temp")
	cBefore := sh.revParse("c")

	sh.Run("abort --force").
		OutputContains("Restored 1 absorb stash entries")

	sh.HasBranches("a", "b", "c", "main")
	require.Equal(t, cBefore, sh.revParse("c"))
	notes, err := os.ReadFile(filepath.Join(sh.scene.Dir, "notes.txt"))
	require.NoError(t, err)
	require.Equal(t, "uncommitted work\n", string(notes))

	// The stale file no longer blocks anything: the next abort discards it.
	sh.Run("abort --force").
		OutputContains("conflict finished outside stackit")
	sh.HasBranches("a", "b", "c", "main")
}
