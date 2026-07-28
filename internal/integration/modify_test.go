package integration

import (
	"testing"
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
}
