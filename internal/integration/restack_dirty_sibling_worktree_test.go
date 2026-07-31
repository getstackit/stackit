package integration

import (
	"testing"
)

// TestModifyDoesNotStageRevertInDirtySiblingWorktree covers the half of the
// dirty-worktree guard that lives above the engine.
//
// Moving a branch's ref is only half the operation — the worktree holding that
// branch has to be reset to match. That reset is suppressed while the worktree
// has uncommitted tracked changes, so it cannot discard them, which leaves the
// worktree on the old commit's content under a moved ref. `git status` there
// reports the new commit's files as deleted, and the next `stackit modify -a`
// commits that deletion, silently reverting trunk content into the branch.
//
// `stackit restack` avoids this by holding the branch back entirely
// (skipDirtyWorktreeStacks). But that guard lives in actions.PlanRestack, which
// only the restack command reaches. modify, sync, squash, absorb, delete, split
// and insert all go straight to engine.RestackBranches, where the only
// protection is the reset suppression — so they still move the ref.
//
// Here modify amends `a`, which restacks its upstack branch `b`; `b` is checked
// out in a plain `git worktree add` sibling with a locally modified tracked
// file.
func TestModifyDoesNotStageRevertInDirtySiblingWorktree(t *testing.T) {
	t.Parallel()

	sh := NewTestShellInProcess(t)
	sh.CreateLinearStack3()

	// Park b in its own worktree, the way someone reviewing or building two
	// branches at once would.
	worktreeDir := t.TempDir()
	sh.Checkout("a")
	sh.Git("worktree add " + worktreeDir + " b")

	// Real work in progress there: a tracked file modified, not staged. The
	// stack fixture writes through Write, which prefixes, so b's file is
	// b.txt_test.txt.
	wt := sh.InWorktree(worktreeDir)
	wt.WriteUnstaged("b.txt_test.txt", "work in progress")

	sh.Git("rev-parse b")
	bRevBefore := sh.Output()

	// Amending a restacks b — which is checked out, and dirty, elsewhere.
	sh.Write("a_extra", "more work on a").
		Run("modify -a -n")

	// Either of these is a correct outcome: hold b back so its ref does not
	// move, or move it and reset the worktree. What must not happen is moving
	// the ref and leaving the worktree behind, because that is the state whose
	// next `modify -a` commits a revert.
	status := wt.Git("status --porcelain").Output()
	bRevAfter := sh.Git("rev-parse b").Output()

	if bRevAfter != bRevBefore {
		wt.Git("status --porcelain").
			OutputNotContains("D  ").
			OutputNotContains(" D ")
		t.Logf("b moved %s-> %s, worktree status: %q", bRevBefore, bRevAfter, status)
	}

	// The in-progress edit must survive regardless of which path was taken.
	wt.Git("show :b.txt_test.txt")
}
