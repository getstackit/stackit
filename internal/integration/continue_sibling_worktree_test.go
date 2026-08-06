package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestContinueResetsRebasedBranchSiblingWorktree covers the half of the
// worktree-sync contract that the continue path skipped.
//
// restackBranch resets a branch's worktree after moving its ref, because moving
// the ref alone leaves that worktree on the pre-rebase content: `git status`
// there reports the rebased commit's files as deleted, and the next
// `stackit modify -a` in that worktree commits the deletion, silently reverting
// the rebase. ContinueRebase moved the ref the same way but never reset, so
// every conflict resolved on a branch checked out elsewhere left that worktree
// diverged.
func TestContinueResetsRebasedBranchSiblingWorktree(t *testing.T) {
	t.Parallel()

	sh := NewTestShellInProcess(t)

	// main -> a -> b, with b parked in its own worktree the way someone
	// building two branches at once would have it.
	sh.WriteFile("common.txt", "base").Run("create a -m 'a'")
	sh.WriteFile("common.txt", "base\nb content").Run("create b -m 'b'")

	worktreeDir := t.TempDir()
	sh.Checkout("a")
	sh.Git("worktree add " + worktreeDir + " b")
	wt := sh.InWorktree(worktreeDir)

	// Amend a so restacking b conflicts.
	sh.WriteFile("common.txt", "base\na conflicting change").
		Git("commit --amend --no-edit")

	sh.RunExpectError("restack --upstack").
		OutputContains("conflict")

	// Resolve and continue. b's ref moves here, in the main worktree, while b
	// itself is checked out in the sibling.
	sh.WriteFile("common.txt", "base\na conflicting change\nb content").
		Run("continue")

	// The sibling worktree must match b's new tip. If the reset was skipped it
	// still holds the pre-rebase tree, and status reports the rebased content
	// as modified or deleted.
	wt.Git("status --porcelain").
		OutputNotContains("D  ").
		OutputNotContains(" D ").
		OutputNotContains(" M ")

	wt.Git("diff HEAD --stat").OutputNotContains("common.txt")

	// The rebased branch is checked out elsewhere, so it cannot be attached
	// here — but the command must still land the user on a branch. Returning
	// success from the detached HEAD `git rebase` left behind means their next
	// commit belongs to nothing.
	sh.Git("symbolic-ref -q HEAD").OutputContains("refs/heads/")
}

// TestContinueDoesNotDestroyUntrackedFileInSiblingWorktree covers the hazard the
// sibling-worktree reset introduced.
//
// `git reset --hard` leaves untracked files alone except in one case: when the
// incoming tree contains a file at that same path, it is overwritten. The file
// was never in git, so there is no reflog, stash or snapshot to recover it. The
// batch restack path guards this through untrackedCollisionHold; the one-off
// reset on the continue path must ask the same question.
func TestContinueDoesNotDestroyUntrackedFileInSiblingWorktree(t *testing.T) {
	t.Parallel()

	sh := NewTestShellInProcess(t)

	sh.WriteFile("common.txt", "base").Run("create a -m 'a'")
	sh.WriteFile("common.txt", "base\nb content").Run("create b -m 'b'")

	worktreeDir := t.TempDir()
	sh.Checkout("a")
	sh.Git("worktree add " + worktreeDir + " b")
	wt := sh.InWorktree(worktreeDir)

	// Amend a so restacking b conflicts AND so the incoming tree introduces
	// shared.txt — the path the sibling worktree is about to hold untracked.
	sh.WriteFile("common.txt", "base\na conflicting change").
		WriteFile("shared.txt", "from a").
		Git("commit --amend --no-edit")

	sh.RunExpectError("restack --upstack").
		OutputContains("conflict")

	// While resolving, the user writes an untracked file in the sibling
	// worktree at exactly that path.
	wt.WriteUnstaged("shared.txt", "PRECIOUS UNSAVED WORK")

	sh.WriteFile("common.txt", "base\na conflicting change\nb content").
		Run("continue")

	// It must survive. Holding the reset leaves the worktree stale, which is
	// recoverable; overwriting this file is not.
	wt.Git("status --porcelain").OutputContains("shared.txt")
	content, err := os.ReadFile(filepath.Join(worktreeDir, "shared.txt"))
	require.NoError(t, err)
	require.Equal(t, "PRECIOUS UNSAVED WORK", strings.TrimSpace(string(content)),
		"untracked file in sibling worktree was destroyed by continue")
}
