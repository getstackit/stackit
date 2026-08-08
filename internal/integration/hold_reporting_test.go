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

// parkTrunkInWorktree leaves the shell on a stack branch (freeing trunk from
// this checkout), parks trunk in a plain git worktree, and puts the remote one
// commit ahead of local trunk so sync has a fast-forward to attempt. Returns the
// worktree directory holding trunk.
//
// "trunk_tracked.txt" is committed on trunk so callers can dirty a tracked path;
// the incoming remote commit writes only "remote_only.txt", so an untracked file
// at any other path is one a reset could not destroy.
func parkTrunkInWorktree(t *testing.T, sh *TestShell) string {
	t.Helper()

	sh.WriteFile("trunk_tracked.txt", "base").
		Git("commit -m 'chore: trunk base'").
		Git("push -u origin main").
		WriteFile("remote_only.txt", "remote work").
		Git("commit -m 'chore: remote trunk commit'").
		Git("push origin main").
		Git("reset --hard HEAD~1")

	// Moving onto a stack branch frees trunk so a worktree can hold it.
	sh.Write("a.txt", "content for a").Run("create a -m 'Add a'")

	worktreeDir := t.TempDir()
	sh.Git("worktree add " + worktreeDir + " main")
	return worktreeDir
}

// TestSyncProceedsWhenTrunkWorktreeHasHarmlessUntrackedFile pins the boundary
// that broke sync: an untracked file in the worktree holding trunk is the
// ordinary state of having written a new file and not staged it. A reset cannot
// destroy it unless the incoming commit writes that same path, so it must not
// stop the fast-forward — let alone abort the whole command.
func TestSyncProceedsWhenTrunkWorktreeHasHarmlessUntrackedFile(t *testing.T) {
	t.Parallel()

	sh := NewTestShellInProcess(t, WithRemote())
	worktreeDir := parkTrunkInWorktree(t, sh)

	// Not a path the incoming commit writes.
	sh.InWorktree(worktreeDir).WriteUnstaged("scratch-notes.txt", "notes")

	sh.Run("sync").
		OutputContains("fast-forwarded").
		OutputNotContains("Held")
}

// TestSyncReportsTrunkHoldInsteadOfFailing covers the other half: a tracked
// change in the trunk worktree genuinely blocks the ref move, but that is a hold
// to report, not a failure to abort on. Cleanup and restack do not depend on
// trunk having moved.
func TestSyncReportsTrunkHoldInsteadOfFailing(t *testing.T) {
	t.Parallel()

	sh := NewTestShellInProcess(t, WithRemote())
	worktreeDir := parkTrunkInWorktree(t, sh)

	// An unstaged edit to a tracked path — a reset would discard it.
	sh.InWorktree(worktreeDir).WriteUnstaged("trunk_tracked.txt", "work in progress")

	// Run, not RunExpectError: the hold must not fail the command.
	sh.Run("sync").
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
