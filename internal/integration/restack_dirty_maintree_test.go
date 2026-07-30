package integration

import (
	"testing"
)

// TestRestackWithUntrackedFileLeavesNoStagedRevert covers a regression where an
// untracked file anywhere in the repo made restack leave a staged deletion of
// trunk's files behind.
//
// The reset that realigns a worktree after its branch ref moves is gated on the
// worktree being clean, so that it cannot discard uncommitted work. Dirtiness
// was measured with `git status --porcelain --untracked-files=normal`, so a
// single untracked file — a scratch note, a build artifact, anything not in
// .gitignore — marked the worktree dirty and suppressed the reset. `git reset
// --hard` cannot touch untracked files, so nothing was being protected: the
// working directory simply kept the old commit's content while the branch ref
// moved on.
//
// The result was invisible until it wasn't. `git status` showed trunk's files
// as staged deletions, and the next `stackit modify -a` committed them, quietly
// reverting trunk content inside the stack.
func TestRestackWithUntrackedFileLeavesNoStagedRevert(t *testing.T) {
	t.Parallel()

	sh := NewTestShellInProcess(t)

	sh.CreateLinearStack3().Checkout("a")

	// Trunk advances so the stack genuinely needs restacking. WriteFile stages
	// under the exact name, which the assertions below rely on.
	sh.Checkout("main").
		WriteFile("trunk1.txt", "trunk content").
		Git("commit -m 'chore: trunk commit'").
		Checkout("a")

	// A single untracked file. Not staged, not tracked, not the user's work in
	// any sense restack should care about.
	sh.WriteUnstaged("scratch.txt", "scratch note")

	sh.Run("restack --upstack")

	// The only thing git should report is the untracked file itself. A "D " or
	// " D" entry here means trunk's content was left staged for deletion.
	sh.Git("status --porcelain").
		OutputContains("?? scratch.txt").
		OutputNotContains(" D ").
		OutputNotContains("D  ")

	// And trunk's file is still really there.
	sh.Git("ls-tree HEAD -- trunk1.txt").OutputContains("trunk1.txt")
	sh.Git("status --porcelain -- trunk1.txt").OutputNotContains("trunk1.txt")
}

// TestRestackSkipsBranchWithTrackedChangesInItsWorktree covers the other half.
//
// When a worktree really does have uncommitted tracked changes, suppressing the
// reset is correct — it protects the user's work — but moving the branch ref
// anyway leaves exactly the state the suppression was meant to avoid: the
// worktree holds the old commit's content under a new ref, so `git status`
// reports the new commit's files as deleted and `stackit modify -a` commits the
// revert.
//
// Restack holds that branch back instead, visibly, until the worktree is clean.
func TestRestackSkipsBranchWithTrackedChangesInItsWorktree(t *testing.T) {
	t.Parallel()

	sh := NewTestShellInProcess(t)

	sh.CreateLinearStack3().Checkout("a")
	sh.Git("rev-parse a")
	revBefore := sh.Output()

	sh.Checkout("main").
		WriteFile("trunk1.txt", "trunk content").
		Git("commit -m 'chore: trunk commit'").
		Checkout("a")

	// Modify a TRACKED file without staging it — real work in progress. The
	// stack fixture writes through Write, which prefixes, so branch a's file is
	// a.txt_test.txt.
	sh.WriteUnstaged("a.txt_test.txt", "locally modified, uncommitted")

	sh.Run("restack --upstack").
		OutputContains("uncommitted changes")

	// The branch must not have moved...
	sh.Git("rev-parse a")
	if sh.Output() != revBefore {
		t.Fatalf("branch a moved despite its worktree having uncommitted changes:\nbefore %safter  %s", revBefore, sh.Output())
	}

	// ...and no staged revert of trunk was left behind.
	sh.Git("status --porcelain").
		OutputNotContains("D  ").
		OutputNotContains(" D ")
}
