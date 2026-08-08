package integration

import (
	"fmt"
	"testing"
)

// TestRestackWithHarmlessUntrackedFileProceeds covers the ordinary state of
// having written a new file and not staged it yet.
//
// `git reset --hard` overwrites an untracked file only when the incoming commit
// contains that same path, and leaves every other untracked file alone. Holding
// the branch for any untracked file at all stopped restacks for a state most
// working repositories are in most of the time.
func TestRestackWithHarmlessUntrackedFileProceeds(t *testing.T) {
	t.Parallel()

	sh := NewTestShellInProcess(t)

	sh.CreateLinearStack3().Checkout("a")

	// Trunk advances so the stack genuinely needs restacking. WriteFile stages
	// under the exact name, which the assertions below rely on.
	sh.Checkout("main").
		WriteFile("trunk1.txt", "trunk content").
		Git("commit -m 'chore: trunk commit'").
		Checkout("a")

	// An untracked file at a path trunk knows nothing about. Nothing incoming
	// can overwrite it.
	sh.WriteUnstaged("scratch.txt", "scratch note")

	sh.Run("restack --upstack")

	// The restack happened: trunk's commit is now in this branch's history.
	sh.Git("ls-tree HEAD -- trunk1.txt").OutputContains("trunk1.txt")

	// The untracked file survived, and no staged revert was left behind.
	sh.Git("status --porcelain").
		OutputContains("?? scratch.txt").
		OutputNotContains(" D ").
		OutputNotContains("D  ")
}

// TestRestackWithManyHarmlessUntrackedFilesProceeds keeps the safety rule
// path-based even in repositories with generated or scratch files. More than
// 200 non-colliding files must not turn collision detection into "unknown" and
// hold this branch or its descendants.
func TestRestackWithManyHarmlessUntrackedFilesProceeds(t *testing.T) {
	t.Parallel()

	sh := NewTestShellInProcess(t)
	sh.CreateLinearStack3().Checkout("a")

	sh.Checkout("main").
		WriteFile("trunk1.txt", "trunk content").
		Git("commit -m 'chore: trunk commit'").
		Checkout("a")

	for i := range 201 {
		sh.WriteUnstaged(fmt.Sprintf("scratch-%03d.txt", i), "scratch note")
	}

	sh.Run("restack --upstack")

	// a and its clean descendants all restacked despite the large harmless set.
	for _, branch := range []string{"a", "b", "c"} {
		sh.Git("ls-tree " + branch + " -- trunk1.txt").OutputContains("trunk1.txt")
	}
	sh.Git("status --porcelain").OutputContains("?? scratch-000.txt")
}

// TestRestackHoldsBranchWhenUntrackedFileWouldBeOverwritten covers the case the
// hold actually exists for.
//
// The incoming commit adds a file at the same path as an untracked file in the
// worktree. `git reset --hard` overwrites it silently, and since the file was
// never in Git there is nothing to recover it from — so the branch is held
// instead and the ref does not move.
func TestRestackHoldsBranchWhenUntrackedFileWouldBeOverwritten(t *testing.T) {
	t.Parallel()

	sh := NewTestShellInProcess(t)

	sh.CreateLinearStack3().Checkout("a")
	sh.Git("rev-parse a")
	revBefore := sh.Output()

	// Trunk advances by adding a file the worktree also has, untracked.
	sh.Checkout("main").
		WriteFile("collide.txt", "from trunk").
		Git("commit -m 'chore: trunk adds collide.txt'").
		Checkout("a")

	sh.WriteUnstaged("collide.txt", "my uncommitted work")

	sh.Run("restack --upstack").
		OutputContains("would be overwritten")

	// The branch must not have moved.
	sh.Git("rev-parse a")
	if sh.Output() != revBefore {
		t.Fatalf("branch a moved even though restacking it would overwrite an untracked file:\nbefore %safter  %s", revBefore, sh.Output())
	}

	// And the file still holds the user's content, not trunk's.
	sh.Git("status --porcelain").OutputContains("?? collide.txt")
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
