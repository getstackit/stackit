package integration

import (
	"testing"
)

// TestRestackWithUntrackedFileHoldsBranchAndLeavesNoStagedRevert covers the
// untracked half of the dirty-worktree hold.
//
// The reset that realigns a worktree after its branch ref moves is gated on the
// worktree being clean, so that it cannot discard uncommitted work. Untracked
// files count: `git reset --hard` overwrites an untracked file whose pathname
// the incoming commit also contains, so skipping the reset is not enough — the
// ref must not move either, or the worktree ends up holding the old commit's
// content under a new ref and `stackit modify -a` commits the revert.
//
// So a stray untracked file holds the branch back rather than being restacked
// around it. The file survives, the ref stays put, and nothing is staged.
func TestRestackWithUntrackedFileHoldsBranchAndLeavesNoStagedRevert(t *testing.T) {
	t.Parallel()

	sh := NewTestShellInProcess(t)

	sh.CreateLinearStack3().Checkout("a")
	sh.Git("rev-parse a")
	revBefore := sh.Output()

	// Trunk advances so the stack genuinely needs restacking. WriteFile stages
	// under the exact name, which the assertions below rely on.
	sh.Checkout("main").
		WriteFile("trunk1.txt", "trunk content").
		Git("commit -m 'chore: trunk commit'").
		Checkout("a")

	// A single untracked file — a scratch note, a build artifact, anything not
	// in .gitignore.
	sh.WriteUnstaged("scratch.txt", "scratch note")

	sh.Run("restack --upstack")

	// The branch must not have moved: holding it back is what keeps the
	// worktree and its ref consistent.
	sh.Git("rev-parse a")
	if sh.Output() != revBefore {
		t.Fatalf("branch a moved despite its worktree having an untracked file:\nbefore %safter  %s", revBefore, sh.Output())
	}

	// The only thing git should report is the untracked file itself. A "D " or
	// " D" entry here means trunk's content was left staged for deletion.
	sh.Git("status --porcelain").
		OutputContains("?? scratch.txt").
		OutputNotContains(" D ").
		OutputNotContains("D  ")
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
