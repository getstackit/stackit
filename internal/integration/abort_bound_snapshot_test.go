package integration

import (
	"os"
	"os/exec"
	"path/filepath"
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
