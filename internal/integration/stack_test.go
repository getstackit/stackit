package integration

import (
	"testing"
)

// =============================================================================
// Stack Workflow Integration Tests
//
// These tests cover basic stack operations: creating branches, amending,
// restacking, squashing, and working with parallel branch structures.
// =============================================================================

func TestStackWorkflow(t *testing.T) {
	t.Parallel()

	t.Run("full stack workflow: create, amend, restack, squash", func(t *testing.T) {
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

		sh.Run("log --stack").
			OutputContains("feature-a").
			OutputContains("feature-b").
			OutputContains("feature-c")

		// Add commits and amend on feature-a
		sh.Log("Adding commits and amending on feature-a...")
		sh.Checkout("feature-a").
			Commit("feature_a_extra", "additional work").
			CommitCount("main", "feature-a", 2).
			Amend("feature_a_amended", "amended content")

		// Restack to propagate changes
		sh.Log("Restacking upstack branches...")
		sh.Run("restack --upstack")

		// Verify children are still valid
		sh.Checkout("feature-b").
			Run("info").
			OutputContains("feature-b")

		sh.Checkout("feature-c").
			Run("info").
			OutputContains("feature-c")

		// Squash commits on feature-a
		sh.Log("Squashing commits on feature-a...")
		sh.Checkout("feature-a").
			CommitCount("main", "feature-a", 2).
			Run("squash -m 'Feature A complete'").
			CommitCount("main", "feature-a", 1)

		// Verify the squashed commit message
		sh.Run("info").
			OutputContains("Feature A complete")

		// Verify children survived the squash
		sh.Log("Verifying children are still valid after squash...")
		sh.Checkout("feature-b").
			Run("info").
			OutputContains("feature-b")

		sh.Checkout("feature-c").
			Run("info").
			OutputContains("feature-c")

		sh.HasBranches("feature-a", "feature-b", "feature-c", "main")
		sh.Log("✓ Full workflow complete!")
	})

	t.Run("stack workflow with parallel branches", func(t *testing.T) {
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

		// Amend feature-a and restack everything
		sh.Log("Amending feature-a and restacking...")
		sh.Checkout("feature-a").
			Amend("a_amended", "feature a amended").
			Run("restack --upstack")

		// Verify all branches survived
		sh.Checkout("feat-b1").Run("info").OutputContains("feat-b1")
		sh.Checkout("feat-b2").Run("info").OutputContains("feat-b2")
		sh.Checkout("feature-c").Run("info").OutputContains("feature-c")

		sh.Log("✓ Parallel branch workflow complete!")
	})
}
