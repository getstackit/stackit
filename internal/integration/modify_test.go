package integration

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestModifyWorkflow(t *testing.T) {
	t.Parallel()

	t.Run("modify amends and restacks upstack branches", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		sh.Write("feature_a", "feature a content").
			Run("create feature-a -m 'Add feature A'").
			OnBranch("feature-a")

		sh.Write("feature_b", "feature b content").
			Run("create feature-b -m 'Add feature B'").
			OnBranch("feature-b")

		sh.Write("feature_c", "feature c content").
			Run("create feature-c -m 'Add feature C'").
			OnBranch("feature-c")

		sh.Run("tree --stack").
			OutputContains("feature-a").
			OutputContains("feature-b").
			OutputContains("feature-c")

		sh.Checkout("feature-a").
			Modify("feature_a_updated", "updated content").
			OutputContains("Amended commit").
			OutputContains("Restacking")

		sh.Checkout("feature-b").
			Run("info").
			OutputContains("feature-b")

		sh.Checkout("feature-c").
			Run("info").
			OutputContains("feature-c")

		sh.Git("show feature-b:feature_b_test.txt").
			OutputContains("feature b content")

		sh.Git("show feature-c:feature_c_test.txt").
			OutputContains("feature c content")
	})

	t.Run("modify with --commit creates new commit and restacks", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		sh.Write("a", "a content").
			Run("create feature-a -m 'Feature A'").
			OnBranch("feature-a")

		sh.Write("b", "b content").
			Run("create feature-b -m 'Feature B'").
			OnBranch("feature-b")

		sh.Checkout("feature-a").
			CommitCount("main", "feature-a", 1)

		sh.Write("a_extra", "extra content").
			Run("modify -c -m 'Additional work on A'").
			OutputContains("Created new commit").
			CommitCount("main", "feature-a", 2)

		sh.Checkout("feature-b").
			Run("info").
			OutputContains("feature-b")

		sh.Git("show feature-b:b_test.txt").
			OutputContains("b content")
	})

	t.Run("modify in diamond-shaped stack restacks all children", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		sh.Write("a", "feature a").Run("create feature-a -m 'Feature A'")
		sh.Write("b1", "feature b1").Run("create feat-b1 -m 'Feature B1'")

		sh.Checkout("feature-a")
		sh.Write("b2", "feature b2").Run("create feat-b2 -m 'Feature B2'")
		sh.Write("c", "feature c").Run("create feature-c -m 'Feature C'")

		sh.HasBranches("feat-b1", "feat-b2", "feature-a", "feature-c", "main")

		sh.Checkout("feature-a").
			Modify("a_updated", "updated feature a").
			OutputContains("Restacking")

		sh.Checkout("feat-b1").Run("info").OutputContains("feat-b1")
		sh.Checkout("feat-b2").Run("info").OutputContains("feat-b2")
		sh.Checkout("feature-c").Run("info").OutputContains("feature-c")
	})
}

func TestModifyInto(t *testing.T) {
	t.Parallel()

	t.Run("amends a downstack ancestor without checking it out, then returns", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		sh.CreateLinearStack3().
			OnBranch("c")

		sh.Write("a_extra", "extra content for a").
			Run("modify --into a -n").
			OutputContains("Modifying downstack branch a").
			OutputContains("Amended commit in a").
			OutputContains("Restacking").
			OnBranch("c")

		sh.Git("show a:a_extra_test.txt").
			OutputContains("extra content for a")

		sh.Checkout("b").Run("info").OutputContains("b")
		sh.Checkout("c").Run("info").OutputContains("c")
	})

	t.Run("with --commit creates a new commit on the target and restacks", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		sh.CreateLinearStack3().
			OnBranch("c").
			CommitCount("main", "a", 1)

		sh.Write("a_extra", "extra content").
			Run("modify --into a -c -m 'Additional work on A'").
			OutputContains("Created new commit in a").
			CommitCount("main", "a", 2).
			OnBranch("c")
	})

	t.Run("refuses a target that is not a downstack ancestor", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		sh.CreateDiamondStack().
			OnBranch("child2")

		sh.Write("extra", "extra content").
			RunExpectError("modify --into child1 -n").
			OutputContains("not a downstack ancestor")
	})

	t.Run("refuses the current branch itself", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		sh.CreateLinearStack3().
			OnBranch("c")

		sh.Write("extra", "extra content").
			RunExpectError("modify --into c -n").
			OutputContains("is the current branch")
	})

	t.Run("refuses a nonexistent branch", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		sh.CreateLinearStack3().
			OnBranch("c")

		sh.Write("extra", "extra content").
			RunExpectError("modify --into does-not-exist -n").
			OutputContains("does not exist")
	})

	t.Run("refuses a target checked out in another worktree", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		sh.CreateLinearStack3().
			OnBranch("c")

		worktreeDir := t.TempDir()
		sh.Git("worktree add " + worktreeDir + " a")

		sh.Write("extra", "extra content").
			RunExpectError("modify --into a -n").
			OutputContains("checked out in worktree").
			OutputContains(worktreeDir)

		sh.OnBranch("c")
	})

	t.Run("gives actionable guidance instead of a raw git error on conflicting staged files", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		sh.CreateLinearStack3().
			OnBranch("c")

		// Diverge the shared file between a and c: amend b's commit so the
		// file differs between a's HEAD and c's HEAD, then stage yet another
		// conflicting version on c. Checking out from c to a then requires
		// overwriting the staged change, which git refuses to do silently.
		sh.Checkout("b").
			Modify("a.txt", "b's version of the shared file")

		sh.Checkout("c").
			Write("a.txt", "c's conflicting staged version").
			RunExpectError("modify --into a -n").
			OutputContains("stackit absorb").
			OnBranch("c")

		sh.Git("diff --cached --name-only").
			OutputContains("a.txt_test.txt")
	})

	// #1497: modify took no undo snapshot, so `stackit abort` fell back to
	// whatever unrelated command's snapshot happened to be latest (typically the
	// last `create`) instead of the state right before this modify ran — landing
	// on the wrong branch and, in the worst case, deleting branches created since
	// that stale snapshot.
	t.Run("abort after a conflict restacking upstack returns to the original branch", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		// main -> a -> b -> d. d's own commit reverts b's change to file.txt,
		// so file.txt is identical between a and d: checking out a to amend
		// it hits no local-changes conflict. b's original edit to the same
		// line still conflicts when b is replayed onto the amended a.
		sh.WriteFile("file.txt", "L1\nL2\nL3\n").
			Run("create a -m 'Add a'").
			OnBranch("a")
		sh.WriteFile("file.txt", "L1\nL2-changed\nL3\n").
			Run("create b -m 'Add b'").
			OnBranch("b")
		sh.WriteFile("file.txt", "L1\nL2\nL3\n").
			Run("create d -m 'Add d'").
			OnBranch("d")

		sh.WriteFile("file.txt", "L1\nL2-AMENDED\nL3\n")
		sh.Git("add file.txt")
		sh.RunExpectError("modify --into a -n").
			OutputContains("conflict")

		sh.Run("abort --force").
			OutputContains("Successfully aborted")

		sh.OnBranch("d")
		sh.HasBranches("a", "b", "d", "main")
		sh.Git("show a:file.txt").OutputContains("L1\nL2\nL3\n")
	})

	// #1494: --into promises to amend a downstack ancestor "without checking it
	// out" and put you back. `continue` knew only about the branch it rebased,
	// so resolving a conflict silently changed where you ended up — on the
	// conflicted ancestor rather than the branch you started from.
	t.Run("continue after a conflict restacking upstack returns to the original branch", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		// Same shape as the abort case above: d reverts b's change, so checking
		// out a from d is clean while replaying b onto the amended a conflicts.
		sh.WriteFile("file.txt", "L1\nL2\nL3\n").
			Run("create a -m 'Add a'").
			OnBranch("a")
		sh.WriteFile("file.txt", "L1\nL2-changed\nL3\n").
			Run("create b -m 'Add b'").
			OnBranch("b")
		sh.WriteFile("file.txt", "L1\nL2\nL3\n").
			Run("create d -m 'Add d'").
			OnBranch("d")

		sh.WriteFile("file.txt", "L1\nL2-AMENDED\nL3\n")
		sh.Git("add file.txt")
		sh.RunExpectError("modify --into a -n").
			OutputContains("conflict")

		// Resolve in b's favor, keeping b's exact content so that d — whose own
		// commit reverts b — still applies cleanly and the restack runs to
		// completion rather than stopping on a second conflict.
		sh.WriteFile("file.txt", "L1\nL2-changed\nL3\n")
		sh.Git("add file.txt")
		sh.Run("continue")

		sh.OnBranch("d")
		sh.Git("show a:file.txt").OutputContains("L1\nL2-AMENDED\nL3\n")
	})
}

// TestModifyUndo covers the success-path counterpart to the abort test above:
// with a snapshot in place, `stackit undo` can roll a completed modify back.
func TestModifyUndo(t *testing.T) {
	t.Parallel()

	t.Run("undo restores the pre-amend commit", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		sh.Write("a", "original content").
			Run("create feature-a -m 'Feature A'").
			OnBranch("feature-a")

		before := sh.Git("rev-parse feature-a").Output()

		sh.Modify("a_extra", "extra content").
			OutputContains("Amended commit")

		after := sh.Git("rev-parse feature-a").Output()
		require.NotEqual(t, before, after, "modify should have amended the commit")

		sh.UndoLatest()

		require.Equal(t, before, sh.Git("rev-parse feature-a").Output(),
			"undo should restore the pre-amend commit")
	})

	t.Run("undo restores both branches after modify --into", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		sh.CreateLinearStack3().
			OnBranch("c")

		beforeA := sh.Git("rev-parse a").Output()
		beforeB := sh.Git("rev-parse b").Output()

		sh.Write("a_extra", "extra content for a").
			Run("modify --into a -n").
			OutputContains("Amended commit in a").
			OnBranch("c")

		require.NotEqual(t, beforeA, sh.Git("rev-parse a").Output(),
			"modify --into should have amended the target branch")
		require.NotEqual(t, beforeB, sh.Git("rev-parse b").Output(),
			"restacking upstack branches should have moved b")

		sh.UndoLatest()

		require.Equal(t, beforeA, sh.Git("rev-parse a").Output(),
			"undo should restore the target branch's pre-amend commit")
		require.Equal(t, beforeB, sh.Git("rev-parse b").Output(),
			"undo should restore the restacked branch's pre-restack commit")
		sh.OnBranch("c")
	})
}
