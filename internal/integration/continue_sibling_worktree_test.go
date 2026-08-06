package integration

import (
	"testing"
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
}
