package integration

import (
	"testing"
)

// =============================================================================
// Modify Command Integration Tests
//
// These tests verify the modify command works correctly in realistic workflows,
// including automatic restacking of upstack branches.
// =============================================================================

func TestModifyWorkflow(t *testing.T) {
	t.Parallel()

	t.Run("modify amends and restacks upstack branches", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		// Build a stack: main -> feature-a -> feature-b -> feature-c
		sh.Log("Creating stacked branches...")
		sh.Write("feature_a", "feature a content").
			Run("create feature-a -m 'Add feature A'").
			OnBranch("feature-a")

		sh.Write("feature_b", "feature b content").
			Run("create feature-b -m 'Add feature B'").
			OnBranch("feature-b")

		sh.Write("feature_c", "feature c content").
			Run("create feature-c -m 'Add feature C'").
			OnBranch("feature-c")

		// Verify the stack
		sh.Run("log --stack").
			OutputContains("feature-a").
			OutputContains("feature-b").
			OutputContains("feature-c")

		// Modify feature-a using the modify command
		sh.Log("Modifying feature-a with stackit modify...")
		sh.Checkout("feature-a").
			Modify("feature_a_updated", "updated content").
			OutputContains("Amended commit").
			OutputContains("Restacking")

		// Verify children are still valid after automatic restack
		sh.Log("Verifying upstack branches survived modify...")
		sh.Checkout("feature-b").
			Run("info").
			OutputContains("feature-b")

		sh.Checkout("feature-c").
			Run("info").
			OutputContains("feature-c")

		// Verify all files still exist in the stack
		sh.Git("show feature-b:feature_b_test.txt").
			OutputContains("feature b content")

		sh.Git("show feature-c:feature_c_test.txt").
			OutputContains("feature c content")

		sh.Log("✓ Modify workflow complete!")
	})

	t.Run("modify with --commit creates new commit and restacks", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		// Build a stack: main -> feature-a -> feature-b
		sh.Log("Creating stacked branches...")
		sh.Write("a", "a content").
			Run("create feature-a -m 'Feature A'").
			OnBranch("feature-a")

		sh.Write("b", "b content").
			Run("create feature-b -m 'Feature B'").
			OnBranch("feature-b")

		// Go back to feature-a and create a new commit
		sh.Log("Creating new commit on feature-a with --commit flag...")
		sh.Checkout("feature-a").
			CommitCount("main", "feature-a", 1)

		sh.Write("a_extra", "extra content").
			Run("modify -c -m 'Additional work on A'").
			OutputContains("Created new commit").
			CommitCount("main", "feature-a", 2)

		// Verify feature-b was restacked
		sh.Log("Verifying feature-b was restacked...")
		sh.Checkout("feature-b").
			Run("info").
			OutputContains("feature-b")

		sh.Git("show feature-b:b_test.txt").
			OutputContains("b content")

		sh.Log("✓ Modify with --commit complete!")
	})

	t.Run("modify with message changes commit message", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		// Create a branch
		sh.Write("feature", "feature content").
			Run("create feature -m 'Original message'").
			OnBranch("feature")

		// Verify original message
		sh.Git("log -1 --format=%s").
			OutputContains("Original message")

		// Modify with new message
		sh.Log("Modifying commit message...")
		sh.Write("feature_updated", "updated").
			Run("modify -m 'Updated message'")

		// Verify message changed
		sh.Git("log -1 --format=%s").
			OutputContains("Updated message")

		sh.Log("✓ Modify with message complete!")
	})

	t.Run("modify in diamond-shaped stack restacks all children", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		// Create diamond-shaped stack:
		//        main
		//          |
		//      feature-a
		//       /     \
		//   feat-b1  feat-b2
		//               |
		//           feature-c

		sh.Log("Creating diamond-shaped branch structure...")
		sh.Write("a", "feature a").Run("create feature-a -m 'Feature A'")
		sh.Write("b1", "feature b1").Run("create feat-b1 -m 'Feature B1'")

		sh.Checkout("feature-a")
		sh.Write("b2", "feature b2").Run("create feat-b2 -m 'Feature B2'")
		sh.Write("c", "feature c").Run("create feature-c -m 'Feature C'")

		sh.HasBranches("feat-b1", "feat-b2", "feature-a", "feature-c", "main")

		// Modify feature-a
		sh.Log("Modifying feature-a and verifying all children restacked...")
		sh.Checkout("feature-a").
			Modify("a_updated", "updated feature a").
			OutputContains("Restacking")

		// Verify all children are valid
		sh.Checkout("feat-b1").Run("info").OutputContains("feat-b1")
		sh.Checkout("feat-b2").Run("info").OutputContains("feat-b2")
		sh.Checkout("feature-c").Run("info").OutputContains("feature-c")

		sh.Log("✓ Diamond-shaped modify complete!")
	})

	t.Run("modify with --update only stages tracked files", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		// Create a branch with a tracked file
		sh.Write("tracked", "tracked content").
			Run("create feature -m 'Add tracked file'").
			OnBranch("feature")

		// Create an untracked file (use unstaged=true via direct scene access)
		err := sh.Scene().Repo.CreateChange("untracked content", "untracked", true)
		if err != nil {
			t.Fatal(err)
		}

		// Modify the tracked file (unstaged)
		err = sh.Scene().Repo.CreateChange("modified tracked content", "tracked", true)
		if err != nil {
			t.Fatal(err)
		}

		// Run modify with --update
		sh.Log("Running modify with --update...")
		sh.Run("modify -u -n").
			OutputContains("Amended commit")

		// Verify untracked file is still untracked
		sh.Git("status --porcelain").
			OutputContains("?? untracked")

		sh.Log("✓ Modify with --update complete!")
	})

	t.Run("modify errors on trunk branch", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		// Stay on main (trunk)
		sh.OnBranch("main")

		// Try to modify - should error
		sh.Log("Attempting to modify trunk (should fail)...")
		sh.RunExpectError("modify -a -n").
			OutputContains("cannot modify trunk")

		sh.Log("✓ Trunk protection works!")
	})

	t.Run("modify branching stack restacks all parallel children", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		// Create a branching stack:
		//        main
		//          |
		//       branch-a
		//       /      \
		//   branch-b  branch-c
		//               |
		//            branch-d

		sh.Log("Creating branching stack structure...")
		sh.Write("a1", "content a1").Run("create branch-a -m 'feat: a1'")

		sh.Write("b1", "content b1").Run("create branch-b -m 'feat: b1'")

		sh.Checkout("branch-a")
		sh.Write("c1", "content c1").Run("create branch-c -m 'feat: c1'")

		sh.Write("d1", "content d1").Run("create branch-d -m 'feat: d1'")

		// Modify branch-a
		sh.Log("Modifying branch-a...")
		sh.Checkout("branch-a")
		sh.Write("a1_updated", "updated content a1").Run("modify -n")

		// Verify all children were restacked and are still valid
		sh.Log("Verifying all children were restacked...")

		// Check branch-b
		sh.Checkout("branch-b").Run("info").OutputContains("branch-b")

		// Check branch-c
		sh.Checkout("branch-c").Run("info").OutputContains("branch-c")

		// Check branch-d
		sh.Checkout("branch-d").Run("info").OutputContains("branch-d")

		sh.Log("✓ Branching stack modify complete!")
	})
}
