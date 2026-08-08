package integration

import (
	"testing"
)

// A held branch returns RestackUnneeded — the same status as a branch that
// needed no work — so without an explicit reason "I protected your work" and
// "there was nothing to do" are indistinguishable. The remedy lives in another
// worktree the user is not looking at, so the reason has to name it.
//
// See the "Hold Trunk Never, Report Holds Always" invariant in
// .claude/rules/worktree-safety.md.

// TestModifyReportsHeldBranchInsteadOfUpToDate covers the engine hold reaching
// the user through modify, which restacks upstack branches after amending.
func TestModifyReportsHeldBranchInsteadOfUpToDate(t *testing.T) {
	t.Parallel()

	sh := NewTestShellInProcess(t)
	sh.CreateLinearStack3()

	// Park b in a plain git worktree and leave uncommitted tracked work there,
	// so restacking b would have to reset over it.
	worktreeDir := t.TempDir()
	sh.Checkout("a")
	sh.Git("worktree add " + worktreeDir + " b")
	sh.InWorktree(worktreeDir).WriteUnstaged("b.txt_test.txt", "work in progress")

	sh.Write("a_extra", "more work on a").
		Run("modify -a -n").
		OutputContains("Held").
		OutputContains(worktreeDir).
		OutputContains("uncommitted changes")
}

// TestRestackReportsHeldDescendantInsteadOfUpToDate covers a descendant held
// because its ancestor is: the reason must name the ancestor and carry the
// originating worktree, not report the descendant as up to date.
func TestRestackReportsHeldDescendantInsteadOfUpToDate(t *testing.T) {
	t.Parallel()

	sh := NewTestShellInProcess(t)
	sh.CreateLinearStack3()

	// Trunk advances so the whole stack genuinely needs restacking.
	sh.Checkout("main").
		WriteFile("trunk1.txt", "trunk content").
		Git("commit -m 'chore: trunk commit'")

	// b is held by its own dirty worktree; c is held only because b is.
	worktreeDir := t.TempDir()
	sh.Git("worktree add " + worktreeDir + " b")
	sh.InWorktree(worktreeDir).WriteUnstaged("b.txt_test.txt", "work in progress")

	sh.Checkout("a").Run("restack --upstack").
		OutputContains("Held").
		OutputContains(worktreeDir)

	// c must not be silently counted as already current.
	sh.OutputNotContains("c up to date")
}
