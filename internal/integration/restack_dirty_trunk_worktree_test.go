package integration

import (
	"testing"
)

// A dirty worktree holds the branch it has checked out, because restacking that
// branch would reset the worktree and discard the work. Trunk is the exception:
// restack never moves trunk's ref and never resets trunk's worktree, so there is
// nothing to protect.
//
// Holding it anyway was not a conservative no-op. `branchHeldBack` walks a
// branch's parent chain and stops at the first held ancestor, and every stacked
// branch's chain terminates at trunk — so one uncommitted tracked change in a
// worktree parked on trunk held every trunk-rooted branch in the repository,
// and the commands reported success while doing nothing.

// TestRestackProceedsWhenTrunkWorktreeIsDirty covers the restack command, which
// at least printed a warning — but then claimed "Everything is up to date!" and
// exited 0 with the stack still unrestacked.
func TestRestackProceedsWhenTrunkWorktreeIsDirty(t *testing.T) {
	t.Parallel()

	sh := NewTestShellInProcess(t)
	sh.CreateLinearStack3()

	// Trunk advances so the stack genuinely needs restacking, and leaves a
	// tracked file on trunk for the next step to dirty.
	sh.Checkout("main").
		WriteFile("trunk1.txt", "trunk content").
		Git("commit -m 'chore: trunk commit'")

	// Uncommitted tracked work in the worktree that holds trunk. Restack never
	// touches this worktree, so it is not at risk either way.
	sh.WriteUnstaged("trunk1.txt", "work in progress on trunk")

	sh.Run("restack --all-stacks")

	// Every branch restacked onto the new trunk. Asserted against the refs
	// rather than by checking each branch out, so the assertion is about the
	// restack and not about whether checkout tolerates the dirty trunk file.
	for _, branch := range []string{"a", "b", "c"} {
		sh.Git("ls-tree " + branch + " -- trunk1.txt").
			OutputContains("trunk1.txt")
	}

	// And the uncommitted work on trunk survived untouched.
	sh.Git("status --porcelain").OutputContains("trunk1.txt")
	sh.Git("show :trunk1.txt").OutputContains("trunk content")
}

// TestModifyRestacksChildrenWhenTrunkWorktreeIsDirty covers the silent half.
//
// modify, sync, squash, absorb, split and reorder go straight to
// engine.RestackBranches and never reach the restack command's warning, so this
// reported "b up to date" for a branch it had left stale — the shape that gets
// a stale child submitted.
func TestModifyRestacksChildrenWhenTrunkWorktreeIsDirty(t *testing.T) {
	t.Parallel()

	sh := NewTestShellInProcess(t)
	sh.CreateLinearStack3()

	// The main worktree stays parked on trunk with uncommitted tracked work —
	// the ordinary state after `stackit create -w`, which checks the main repo
	// back out to trunk. Work happens in a sibling worktree on `a`.
	sh.Checkout("main").
		WriteFile("trunk1.txt", "trunk content").
		Git("commit -m 'chore: trunk commit'").
		WriteUnstaged("trunk1.txt", "work in progress on trunk")

	worktreeDir := t.TempDir()
	sh.Git("worktree add " + worktreeDir + " a")
	wt := sh.InWorktree(worktreeDir)

	// Amending a must restack b onto it.
	wt.Write("a_extra", "more work on a").
		Run("modify -a -n")

	aRev := sh.Git("rev-parse a").Output()
	sh.Git("merge-base --is-ancestor a b")

	// b must contain the amended a. If the hold fired, b still points at the
	// pre-amend a and this is the stale state modify claimed was up to date.
	sh.Git("log b --format=%H").OutputContains(aRev)
}

// TestModifyRestacksCleanChildrenWhenCurrentWorktreeIsDirty covers the case
// where modify has already moved the current branch's ref. Its own worktree is
// necessarily dirty until the staged amendment and unrelated unstaged edit are
// separated again, but clean descendants must still rebase onto that new ref.
func TestModifyRestacksCleanChildrenWhenCurrentWorktreeIsDirty(t *testing.T) {
	t.Parallel()

	sh := NewTestShellInProcess(t)
	sh.CreateLinearStack3().Checkout("a")

	// This staged change is the amendment. The tracked, unstaged edit is
	// unrelated work in progress that must hold a's worktree, not b or c.
	sh.Write("a_extra", "amended content").
		WriteUnstaged("a.txt_test.txt", "work in progress on a").
		Run("modify -n")

	// b and c have no worktrees of their own, so they must be rebuilt on a's
	// new tip rather than reported as up to date because a is held.
	sh.Git("merge-base --is-ancestor a b")
	sh.Git("merge-base --is-ancestor b c")
	sh.Git("status --porcelain").OutputContains("a.txt_test.txt")
}

// TestSquashRestacksCleanChildrenWhenCurrentWorktreeIsDirty exercises the
// same validated-plan application path for squash. Squash moves a's ref while
// its unrelated tracked edit remains unstaged, then b must rebase onto a.
func TestSquashRestacksCleanChildrenWhenCurrentWorktreeIsDirty(t *testing.T) {
	t.Parallel()

	sh := NewTestShellInProcess(t)
	sh.Write("a", "first commit").Run("create a -m 'feat: a'")
	sh.Write("a_extra", "second commit").Run("modify -c -m 'feat: a extra'")
	sh.Write("b", "child commit").Run("create b -m 'feat: b'")

	sh.Checkout("a").
		WriteUnstaged("a_test.txt", "work in progress on a").
		Run("squash --no-edit")

	sh.Git("merge-base --is-ancestor a b")
	sh.Git("status --porcelain").OutputContains("a_test.txt")
}
