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

	t.Run("scope inheritance in stacked branches", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		// Create a stack with scope inheritance
		sh.Log("Creating stack with scope inheritance...")
		sh.Write("parent", "parent content").
			Run("create parent-branch --scope PROJ-123 -m 'Add parent feature'").
			OnBranch("parent-branch")

		sh.Write("child", "child content").
			Run("create child-branch -m 'Add child feature'"). // Should inherit PROJ-123
			OnBranch("child-branch")

		sh.Write("grandchild", "grandchild content").
			Run("create grandchild-branch -m 'Add grandchild feature'"). // Should inherit PROJ-123
			OnBranch("grandchild-branch")

		// Verify scope inheritance
		sh.Log("Verifying scope inheritance...")
		sh.Checkout("parent-branch").
			Run("scope --show").
			OutputContains("explicit scope: PROJ-123")

		sh.Checkout("child-branch").
			Run("scope --show").
			OutputContains("inherits scope: PROJ-123")

		sh.Checkout("grandchild-branch").
			Run("scope --show").
			OutputContains("inherits scope: PROJ-123").
			OutputContains("child-branch")

		sh.Log("✓ Scope inheritance test complete!")
	})

	t.Run("scope override in middle of stack", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		// Create a stack where middle branch overrides scope
		sh.Log("Creating stack with scope override...")
		sh.Write("base", "base content").
			Run("create base-branch --scope PROJ-456 -m 'Add base feature'").
			OnBranch("base-branch")

		sh.Write("middle", "middle content").
			Run("create middle-branch --scope PROJ-789 -m 'Add middle feature'"). // Override to different scope
			OnBranch("middle-branch")

		sh.Write("top", "top content").
			Run("create top-branch -m 'Add top feature'"). // Should inherit from middle (PROJ-789)
			OnBranch("top-branch")

		// Verify scope override and inheritance
		sh.Log("Verifying scope override...")
		sh.Checkout("base-branch").
			Run("scope --show").
			OutputContains("explicit scope: PROJ-456")

		sh.Checkout("middle-branch").
			Run("scope --show").
			OutputContains("explicit scope: PROJ-789")

		sh.Checkout("top-branch").
			Run("scope --show").
			OutputContains("inherits scope: PROJ-789")

		sh.Log("✓ Scope override test complete!")
	})

	t.Run("scope inheritance break with none", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		// Create a stack where middle branch breaks inheritance
		sh.Log("Creating stack with scope inheritance break...")
		sh.Write("scoped", "scoped content").
			Run("create scoped-branch --scope PROJ-999 -m 'Add scoped feature'").
			OnBranch("scoped-branch")

		sh.Write("broken", "broken content").
			Run("create broken-branch --scope none -m 'Add broken feature'"). // Break inheritance
			OnBranch("broken-branch")

		sh.Write("unscoped", "unscoped content").
			Run("create unscoped-branch -m 'Add unscoped feature'"). // No scope since inheritance broken
			OnBranch("unscoped-branch")

		// Verify scope inheritance break
		sh.Log("Verifying scope inheritance break...")
		sh.Checkout("scoped-branch").
			Run("scope --show").
			OutputContains("explicit scope: PROJ-999")

		sh.Checkout("broken-branch").
			Run("scope --show").
			OutputContains("scope inheritance DISABLED").
			OutputContains("explicitly set to 'none'")

		sh.Checkout("unscoped-branch").
			Run("scope --show").
			OutputContains("no scope set")

		sh.Log("✓ Scope inheritance break test complete!")
	})

	t.Run("mixed scope repository with multiple stacks", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		// Create multiple independent stacks with different scopes
		sh.Log("Creating mixed scope repository...")

		// PROJ-111 stack
		sh.Write("proj111_base", "PROJ-111 base content").
			Run("create proj111-base --scope PROJ-111 -m 'PROJ-111: Base feature'").
			OnBranch("proj111-base")

		sh.Write("proj111_feature", "PROJ-111 feature content").
			Run("create proj111-feature -m 'PROJ-111: Additional feature'").
			OnBranch("proj111-feature")

		// PROJ-222 stack (parallel to PROJ-111)
		sh.Checkout("main").
			Write("proj222_base", "PROJ-222 base content").
			Run("create proj222-base --scope PROJ-222 -m 'PROJ-222: Base feature'").
			OnBranch("proj222-base")

		sh.Write("proj222_feature", "PROJ-222 feature content").
			Run("create proj222-feature -m 'PROJ-222: Additional feature'").
			OnBranch("proj222-feature")

		// PROJ-333 stack (parallel to others)
		sh.Checkout("main").
			Write("proj333_base", "PROJ-333 base content").
			Run("create proj333-base --scope PROJ-333 -m 'PROJ-333: Base feature'").
			OnBranch("proj333-base")

		// Verify all branches exist
		sh.HasBranches("proj111-base", "proj111-feature", "proj222-base", "proj222-feature", "proj333-base", "main")

		// Verify scopes are correctly set
		sh.Log("Verifying scopes in mixed repository...")
		// Batch scope checks by staying on each branch and checking multiple things
		sh.Checkout("proj111-base").
			Run("scope --show").
			OutputContains("explicit scope: PROJ-111")

		sh.Checkout("proj111-feature").
			Run("scope --show").
			OutputContains("inherits scope: PROJ-111")

		sh.Checkout("proj222-base").
			Run("scope --show").
			OutputContains("explicit scope: PROJ-222")

		sh.Checkout("proj222-feature").
			Run("scope --show").
			OutputContains("inherits scope: PROJ-222")

		sh.Checkout("proj333-base").
			Run("scope --show").
			OutputContains("explicit scope: PROJ-333")

		// Test scope-based operations work correctly
		sh.Log("Testing scope isolation...")
		// Each scope should operate independently
		// (Note: We can't easily test merge --scope without PR setup, but we can test basic scope commands)

		sh.Log("✓ Mixed scope repository test complete!")
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
