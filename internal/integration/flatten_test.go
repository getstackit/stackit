package integration

import (
	"testing"
)

// =============================================================================
// Flatten Workflow Integration Tests
//
// These tests cover the flatten command which moves branches closer to trunk
// when they don't actually depend on their parent's changes.
// =============================================================================

func TestFlattenWorkflow(t *testing.T) {
	t.Parallel()

	t.Run("respects dependencies and keeps branch in place", func(t *testing.T) {
		t.Parallel()

		// Scenario:
		// main -> A (adds file-a) -> B (modifies file-a)
		// B depends on A, so B should NOT move to main

		sh := NewTestShellInProcess(t)

		// Create A with a new file
		sh.Write("file-a", "original content").
			Run("create branch-a -m 'Add file A'")

		// Create B which modifies A's file (creating a dependency)
		sh.WriteFile("file-a_test.txt", "modified by B").
			Run("create branch-b -m 'Modify file A'")

		// Verify initial structure
		sh.ExpectStackStructure(map[string]string{
			"branch-a": "main",
			"branch-b": "branch-a",
		})

		// Run flatten
		sh.Run("flatten --yes")

		// A should move to main (independent)
		// B should stay on A (depends on A's file)
		sh.ExpectStackStructure(map[string]string{
			"branch-a": "main",
			"branch-b": "branch-a",
		})
	})

	t.Run("partial flatten with mixed dependencies", func(t *testing.T) {
		t.Parallel()

		// Scenario:
		// main -> A (independent) -> B (depends on A) -> C (independent)
		// After: main -> A, A -> B, main -> C

		sh := NewTestShellInProcess(t)

		// Create independent A
		sh.Write("file-a", "content a").
			Run("create branch-a -m 'Add file A'")

		// Create B that depends on A
		sh.WriteFile("file-a_test.txt", "modified by B").
			Run("create branch-b -m 'Modify file A in B'")

		// Create independent C (only touches its own file)
		sh.Write("file-c", "content c").
			Run("create branch-c -m 'Add file C'")

		// Run flatten
		sh.Run("flatten --yes")

		// A should be on main (independent)
		// B should stay on A (depends on A's file)
		// C should move to main (independent of B)
		sh.ExpectStackStructure(map[string]string{
			"branch-a": "main",
			"branch-b": "branch-a",
			"branch-c": "main",
		})
	})

	t.Run("handles already flat stack", func(t *testing.T) {
		t.Parallel()

		// Scenario: main -> A, main -> B (already flat)
		// Running flatten should be a no-op

		sh := NewTestShellInProcess(t)

		// Create two branches directly from main
		sh.Write("file-a", "content a").
			Run("create branch-a -m 'Add file A'")

		sh.Checkout("main").
			Write("file-b", "content b").
			Run("create branch-b -m 'Add file B'")

		// Verify initial structure (already flat)
		sh.ExpectStackStructure(map[string]string{
			"branch-a": "main",
			"branch-b": "main",
		})

		// Run flatten
		sh.Run("flatten --yes")

		// Structure should remain unchanged
		sh.ExpectStackStructure(map[string]string{
			"branch-a": "main",
			"branch-b": "main",
		})
	})

	t.Run("accepts current branch or positional branch argument", func(t *testing.T) {
		t.Parallel()
		for _, tt := range []struct {
			name string
			prep func(*TestShell)
			cmd  string
		}{
			{
				"uses current branch when none specified",
				func(sh *TestShell) { sh.OnBranch("branch-b") },
				"flatten --yes",
			},
			{
				"positional branch argument",
				func(sh *TestShell) { sh.Checkout("main") },
				"flatten branch-b --yes",
			},
		} {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				sh := NewTestShellInProcess(t)
				sh.Write("file-a", "content a").Run("create branch-a -m 'Add file A'")
				sh.Write("file-b", "content b").Run("create branch-b -m 'Add file B'")
				tt.prep(sh)
				sh.Run(tt.cmd)
				sh.ExpectStackStructure(map[string]string{
					"branch-a": "main",
					"branch-b": "main",
				})
			})
		}
	})

	t.Run("error on untracked branch", func(t *testing.T) {
		t.Parallel()

		sh := NewTestShellInProcess(t)

		// Create an untracked branch using git directly
		sh.Git("checkout -b untracked-branch").
			Write("file", "content")

		// Try to flatten - should fail
		sh.RunExpectError("flatten --yes").
			OutputContains("not tracked")
	})
}

func TestFlattenUndo(t *testing.T) {
	t.Parallel()

	t.Run("undo restores original stack structure", func(t *testing.T) {
		t.Parallel()

		sh := NewTestShellInProcess(t)

		// Create a stack
		sh.Write("file-a", "content a").
			Run("create branch-a -m 'Add file A'")

		sh.Write("file-b", "content b").
			Run("create branch-b -m 'Add file B'")

		// Verify initial structure
		sh.ExpectStackStructure(map[string]string{
			"branch-a": "main",
			"branch-b": "branch-a",
		})

		// Flatten
		sh.Run("flatten --yes")

		// Verify flattened structure
		sh.ExpectStackStructure(map[string]string{
			"branch-a": "main",
			"branch-b": "main",
		})

		// Undo
		sh.UndoLatest()

		// Structure should be restored
		sh.ExpectStackStructure(map[string]string{
			"branch-a": "main",
			"branch-b": "branch-a",
		})
	})
}

func TestFlattenCommitIntegrity(t *testing.T) {
	t.Parallel()

	t.Run("flattened branch only contains its own commits", func(t *testing.T) {
		t.Parallel()

		// Scenario: main -> A -> B -> C (independent changes)
		// After flatten: main -> A, main -> B, main -> C
		// Each branch should have exactly 1 commit relative to main

		sh := NewTestShellInProcess(t)

		sh.Write("file-a", "content a").
			Run("create branch-a -m 'Add file A'")

		sh.Write("file-b", "content b").
			Run("create branch-b -m 'Add file B'")

		sh.Write("file-c", "content c").
			Run("create branch-c -m 'Add file C'")

		// Verify commit counts before flatten
		sh.CommitCount("main", "branch-a", 1).
			CommitCount("main", "branch-b", 2). // B has A's commit + B's commit
			CommitCount("main", "branch-c", 3)  // C has A + B + C

		sh.Run("flatten --yes")

		// After flatten, each branch should have exactly 1 commit relative to main
		sh.CommitCount("main", "branch-a", 1).
			CommitCount("main", "branch-b", 1).
			CommitCount("main", "branch-c", 1)
	})

	t.Run("flattened branch only contains its own commits when main has advanced", func(t *testing.T) {
		t.Parallel()

		// Scenario: User hasn't synced - main has new commits locally
		// main -> A -> B (independent changes), then main gets a new commit
		// After flatten: B should have exactly 1 commit relative to main

		sh := NewTestShellInProcess(t)

		sh.Write("file-a", "content a").
			Run("create branch-a -m 'Add file A'")

		sh.Write("file-b", "content b").
			Run("create branch-b -m 'Add file B'")

		// Advance main (simulating a local pull or direct commit)
		sh.Checkout("main").
			Git("commit --allow-empty -m 'main: advance'")

		// Go back to branch-b and flatten
		sh.Checkout("branch-b").
			Run("flatten --yes")

		sh.ExpectStackStructure(map[string]string{
			"branch-a": "main",
			"branch-b": "main",
		})

		// Each branch should have exactly 1 commit relative to main
		sh.CommitCount("main", "branch-a", 1).
			CommitCount("main", "branch-b", 1)
	})

	t.Run("flattened branch excludes parent commits when parent has extra commits", func(t *testing.T) {
		t.Parallel()

		// Scenario: A has 2 commits, B has 1 commit on top
		// B doesn't depend on A's content so can flatten to main
		// After flatten: B should have exactly 1 commit relative to main

		sh := NewTestShellInProcess(t)

		sh.Write("file-a1", "content a1").
			Run("create branch-a -m 'Add file A1'")

		// Add another commit to A
		sh.Commit("file-a2", "Add file A2")

		// Create B with independent changes
		sh.Write("file-b", "content b").
			Run("create branch-b -m 'Add file B'")

		// Verify: A has 2 commits, B has 3 (2 from A + 1 own)
		sh.CommitCount("main", "branch-a", 2).
			CommitCount("main", "branch-b", 3)

		sh.Run("flatten --yes")

		sh.ExpectStackStructure(map[string]string{
			"branch-a": "main",
			"branch-b": "main",
		})

		// A should have 2 commits, B should have only 1
		sh.CommitCount("main", "branch-a", 2).
			CommitCount("main", "branch-b", 1)
	})

	t.Run("flatten with main advanced and parent amended", func(t *testing.T) {
		t.Parallel()

		// Combined scenario: main advanced + parent amended + no restack
		// This is the most likely scenario for the reported bug

		sh := NewTestShellInProcess(t)

		sh.Write("file-a", "content a").
			Run("create branch-a -m 'Add file A'")

		sh.Write("file-b", "content b").
			Run("create branch-b -m 'Add file B'")

		// Advance main
		sh.Checkout("main").
			Git("commit --allow-empty -m 'main: advance'")

		// Amend A (adds content, changes SHA)
		sh.Checkout("branch-a").
			Amend("file-a-extra", "extra content for A")

		// Do NOT restack branch-b

		// Flatten from B
		sh.Checkout("branch-b").
			Run("flatten --yes")

		sh.ExpectStackStructure(map[string]string{
			"branch-a": "main",
			"branch-b": "main",
		})

		// B should have exactly 1 commit relative to main
		sh.CommitCount("main", "branch-b", 1)
	})

	t.Run("flatten deep stack preserves correct commit counts", func(t *testing.T) {
		t.Parallel()

		// Scenario: main -> A -> B -> C -> D (all independent)
		// After flatten: all on main, each with 1 commit

		sh := NewTestShellInProcess(t)

		sh.Write("file-a", "content a").
			Run("create branch-a -m 'Add file A'")

		sh.Write("file-b", "content b").
			Run("create branch-b -m 'Add file B'")

		sh.Write("file-c", "content c").
			Run("create branch-c -m 'Add file C'")

		sh.Write("file-d", "content d").
			Run("create branch-d -m 'Add file D'")

		// Before flatten
		sh.CommitCount("main", "branch-d", 4)

		sh.Run("flatten --yes")

		sh.ExpectStackStructure(map[string]string{
			"branch-a": "main",
			"branch-b": "main",
			"branch-c": "main",
			"branch-d": "main",
		})

		// Each should have exactly 1 commit
		sh.CommitCount("main", "branch-a", 1).
			CommitCount("main", "branch-b", 1).
			CommitCount("main", "branch-c", 1).
			CommitCount("main", "branch-d", 1)
	})
}

func TestFlattenTreeStructure(t *testing.T) {
	t.Parallel()

	t.Run("flatten handles branching stacks from one path", func(t *testing.T) {
		t.Parallel()

		// Scenario: tree structure with multiple children
		//
		// Before:     main
		//               |
		//           branch-a
		//            /    \
		//       branch-b  branch-c
		//
		// When flattening from branch-c, only branch-c's path is processed.
		// branch-b is a sibling, not in branch-c's upstack/downstack path.
		//
		// After flattening from branch-c:
		//   - branch-a stays on main (already closest)
		//   - branch-b stays on branch-a (not processed - different path)
		//   - branch-c moves to main (independent)

		sh := NewTestShellInProcess(t)

		// Create branch-a from main
		sh.Write("file-a", "content a").
			Run("create branch-a -m 'Add file A'")

		// Create branch-b from branch-a
		sh.Write("file-b", "content b").
			Run("create branch-b -m 'Add file B'")

		// Go back to branch-a and create branch-c
		sh.Checkout("branch-a").
			Write("file-c", "content c").
			Run("create branch-c -m 'Add file C'")

		// Verify initial tree structure
		sh.ExpectStackStructure(map[string]string{
			"branch-a": "main",
			"branch-b": "branch-a",
			"branch-c": "branch-a",
		})

		// Flatten from branch-c
		sh.Run("flatten --yes")

		// branch-c and branch-a are processed
		// branch-b is on a different path (sibling), so it's not processed
		sh.ExpectStackStructure(map[string]string{
			"branch-a": "main",
			"branch-b": "branch-a", // Not processed - sibling branch
			"branch-c": "main",     // Moved to main - independent
		})
	})

	t.Run("flatten from common ancestor processes all descendants", func(t *testing.T) {
		t.Parallel()

		// When we flatten from the common ancestor (branch-a), all children
		// should be considered for flattening.
		//
		// Before:     main
		//               |
		//           branch-a
		//            /    \
		//       branch-b  branch-c
		//
		// After flattening from branch-a: all independent branches move to main

		sh := NewTestShellInProcess(t)

		// Create branch-a from main
		sh.Write("file-a", "content a").
			Run("create branch-a -m 'Add file A'")

		// Create branch-b from branch-a
		sh.Write("file-b", "content b").
			Run("create branch-b -m 'Add file B'")

		// Go back to branch-a and create branch-c
		sh.Checkout("branch-a").
			Write("file-c", "content c").
			Run("create branch-c -m 'Add file C'")

		// Flatten from branch-a (the common ancestor)
		sh.Checkout("branch-a").
			Run("flatten --yes")

		// All should be independent and on main
		sh.ExpectStackStructure(map[string]string{
			"branch-a": "main",
			"branch-b": "main",
			"branch-c": "main",
		})
	})
}

func TestFlattenAncestorAndDescendantMoves(t *testing.T) {
	t.Parallel()

	t.Run("ancestor move and descendant move to ancestor restack once", func(t *testing.T) {
		t.Parallel()

		sh := NewTestShellInProcess(t)

		// main -> x -> a -> b -> c
		// a is independent from x, b depends on a, and c depends on a but not b.
		// Flattening from c should move a to main and c to a.
		sh.WriteFile("shared-1", "base\n").
			WriteFile("shared-2", "base\n").
			Git("commit -m 'Add shared files'")

		sh.Write("file-x", "content x").
			Run("create x -m 'Add file X'")

		sh.WriteFile("shared-1", "a\n").
			WriteFile("shared-2", "a\n").
			Run("create a -m 'Add file A'")

		sh.WriteFile("shared-1", "b\n").
			Run("create b -m 'Modify file A in B'")

		sh.WriteFile("shared-2", "c\n").
			Run("create c -m 'Modify file A in C'")

		sh.Run("flatten --yes")

		sh.ExpectStackStructure(map[string]string{
			"x": "main",
			"a": "main",
			"b": "a",
			"c": "a",
		})
	})
}
