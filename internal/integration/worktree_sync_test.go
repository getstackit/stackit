package integration

import (
	"testing"
)

// TestWorktreeWorkingDirAfterRestack tests that when a branch is restacked
// from a different context, the worktree's working directory is properly updated.
func TestWorktreeWorkingDirAfterRestack(t *testing.T) {
	t.Run("worktree working dir updated after restack from main repo", func(t *testing.T) {
		// This test reproduces a bug where:
		// 1. Branch B is checked out in Worktree W
		// 2. Main advances with new commits
		// 3. Sync runs from main repo (not the worktree)
		// 4. Branch B is rebased (ref updated via UpdateBranchRef)
		// 5. But Worktree W's working directory is NOT updated
		// 6. Result: git shows staged changes that "revert" the new content

		sh := NewTestShellWithRemoteInProcess(t)

		// Create a branch with worktree
		sh.Log("Creating branch with worktree...")
		sh.WriteFile("feature.txt", "feature content").
			Run("create feature -w -m 'feature branch'").
			OnBranch("main")

		// Get worktree path
		worktreePath := sh.GetWorktreePath("feature")
		shW := sh.InWorktree(worktreePath)
		shW.OnBranch("feature")

		// Advance main with a change to a file that the worktree will see after restack
		sh.Log("Advancing main with new file...")
		sh.WriteFile("new-from-main.txt", "content from main").
			Git("add new-from-main.txt").
			Git("commit -m 'Add new file from main'").
			Git("push origin main")

		// Run sync from main repo - this will restack feature onto new main
		sh.Log("Running sync from main repo...")
		sh.Run("sync")

		// Check worktree status - it should be clean after restack
		sh.Log("Checking worktree status...")
		shW.Git("status --porcelain")
		output := shW.Output()

		// The bug: worktree shows staged changes reverting main's content
		// After fix: worktree should be clean
		if output != "" {
			t.Errorf("Worktree should be clean after sync, but has changes:\n%s", output)
		}

		// Verify the new file exists in worktree after restack
		shW.Git("ls-files new-from-main.txt")
		if shW.Output() == "" {
			t.Error("New file from main should exist in worktree after sync/restack")
		}
	})
}

// TestSyncWithMultipleWorktrees tests the sync command behavior when working
// with multiple worktrees and stacked branches.
func TestSyncWithMultipleWorktrees(t *testing.T) {
	t.Run("restack worktree branch after sibling stack merged", func(t *testing.T) {
		// This test reproduces the bug where running st sync from main
		// causes unexpected conflicts when restacking branches in worktrees.
		//
		// Scenario:
		// 1. Create Stack A in worktree A: main -> stackA -> stackA-child
		// 2. Create Stack B in worktree B: main -> stackB -> stackB-child
		// 3. Simulate Stack A getting merged on GitHub
		// 4. Run sync from main repo
		// 5. Expect Stack B to be restacked without unexpected conflicts

		sh := NewTestShellWithRemoteInProcess(t)

		// === Setup: Create two stacks, each in their own worktree ===

		// Stack A: main -> stackA (in worktree A)
		sh.Log("Creating Stack A with worktree...")
		sh.WriteFile("stackA.txt", "stack A content").
			Run("create stackA -w -m 'Stack A root'").
			OnBranch("main") // -w returns to main

		// Get worktree A path and add a child branch
		worktreeA := sh.GetWorktreePath("stackA")
		shA := sh.InWorktree(worktreeA)
		shA.OnBranch("stackA").
			WriteFile("stackA-child.txt", "stack A child content").
			Run("create stackA-child -m 'Stack A child'").
			OnBranch("stackA-child")

		// Stack B: main -> stackB (in worktree B)
		sh.Log("Creating Stack B with worktree...")
		sh.WriteFile("stackB.txt", "stack B content").
			Run("create stackB -w -m 'Stack B root'").
			OnBranch("main")

		// Get worktree B path and add a child branch
		worktreeB := sh.GetWorktreePath("stackB")
		shB := sh.InWorktree(worktreeB)
		shB.OnBranch("stackB").
			WriteFile("stackB-child.txt", "stack B child content").
			Run("create stackB-child -m 'Stack B child'").
			OnBranch("stackB-child")

		// Verify initial stack structure
		sh.Log("Verifying initial stack structure...")
		sh.ExpectBranchParent("stackA", "main")
		sh.ExpectBranchParent("stackA-child", "stackA")
		sh.ExpectBranchParent("stackB", "main")
		sh.ExpectBranchParent("stackB-child", "stackB")

		// === Simulate: Stack A gets merged on GitHub ===
		sh.Log("Simulating Stack A merge on GitHub...")

		// Fast-forward main to include stackA (simulating GitHub squash/merge)
		sh.Git("checkout main").
			Git("merge stackA --ff-only").
			Git("push origin main")

		// Mark stackA PR as merged in metadata
		sh.SetPrState("stackA", "MERGED")

		// === Action: Run sync from main repo ===
		sh.Log("Running sync from main repo...")

		// This should:
		// 1. Pull main (now includes stackA commits)
		// 2. Clean up merged stackA branch
		// 3. Reparent stackA-child to main
		// 4. Restack stackB onto updated main (THIS IS WHERE THE BUG MAY OCCUR)
		sh.OnBranch("main").
			Run("sync")

		// === Verify: No unexpected conflicts ===
		sh.Log("Verifying sync completed without unexpected conflicts...")
		sh.OutputNotContains("conflict")

		// stackB should be rebased onto updated main
		sh.ExpectBranchParent("stackB", "main")
		sh.ExpectBranchParent("stackB-child", "stackB")

		// stackA-child should be reparented to main (since stackA was merged)
		sh.ExpectBranchParent("stackA-child", "main")

		// Verify worktree B is still functional
		sh.Log("Verifying worktree B is still functional...")
		shB.Checkout("stackB-child").
			OnBranch("stackB-child").
			WriteFile("newfile.txt", "new content").
			Run("create stackB-grandchild -m 'New branch works'").
			OnBranch("stackB-grandchild")
	})

	t.Run("restack deep stack in worktree from main repo", func(t *testing.T) {
		// Test that a deep stack in a worktree can be restacked from the main repo
		// when main advances.

		sh := NewTestShellWithRemoteInProcess(t)

		// Create a deep stack in a worktree: main -> a -> b -> c -> d
		sh.Log("Creating deep stack in worktree...")
		sh.WriteFile("a.txt", "a").
			Run("create a -w -m 'branch a'").
			OnBranch("main")

		worktreeA := sh.GetWorktreePath("a")
		shA := sh.InWorktree(worktreeA)
		shA.OnBranch("a").
			WriteFile("b.txt", "b").
			Run("create b -m 'branch b'").
			OnBranch("b").
			WriteFile("c.txt", "c").
			Run("create c -m 'branch c'").
			OnBranch("c").
			WriteFile("d.txt", "d").
			Run("create d -m 'branch d'").
			OnBranch("d")

		// Advance main in the main repo
		sh.Log("Advancing main...")
		sh.WriteFile("main-update.txt", "main change").
			Git("add main-update.txt").
			Git("commit -m 'Main advanced'").
			Git("push origin main")

		// Sync from main - should restack entire worktree stack
		sh.Log("Running sync from main repo...")
		sh.Run("sync")

		// Verify all branches restacked without conflict
		sh.Log("Verifying no conflicts...")
		sh.OutputNotContains("conflict")

		// Verify stack structure is maintained
		sh.ExpectStackStructure(map[string]string{
			"a": "main",
			"b": "a",
			"c": "b",
			"d": "c",
		})
	})

	t.Run("sync from worktree context", func(t *testing.T) {
		// Test that sync works correctly when run from within a worktree
		// (the opposite direction of the main bug)

		sh := NewTestShellWithRemoteInProcess(t)

		// Create a stack in a worktree
		sh.Log("Creating stack in worktree...")
		sh.WriteFile("feature.txt", "feature").
			Run("create feature -w -m 'feature branch'").
			OnBranch("main")

		worktree := sh.GetWorktreePath("feature")
		shW := sh.InWorktree(worktree)
		shW.OnBranch("feature").
			WriteFile("child.txt", "child").
			Run("create feature-child -m 'feature child'")

		// Advance main in the main repo
		sh.Log("Advancing main...")
		sh.WriteFile("main-update.txt", "main change").
			Git("add main-update.txt").
			Git("commit -m 'Main advanced'").
			Git("push origin main")

		// Sync from the worktree context
		sh.Log("Running sync from worktree...")
		shW.Checkout("feature").
			Run("sync")

		// Verify no conflicts
		shW.OutputNotContains("conflict")

		// Verify stack structure
		sh.ExpectStackStructure(map[string]string{
			"feature":       "main",
			"feature-child": "feature",
		})
	})

	t.Run("bottom branch merged in worktree stack keeps worktree alive", func(t *testing.T) {
		// Test scenario:
		// 1. Create stack main -> A -> B -> C in a worktree (A is root)
		// 2. Merge A on GitHub
		// 3. Run sync from main repo
		// Expected:
		// - A should be deleted
		// - B should be reparented to main (becomes new stack root)
		// - Worktree should NOT be deleted (B and C still exist)
		// - Worktree should have B checked out after sync

		sh := NewTestShellWithRemoteInProcess(t)

		// Create stack A -> B -> C in a worktree
		sh.Log("Creating stack A -> B -> C in worktree...")
		sh.WriteFile("a.txt", "a content").
			Run("create branchA -w -m 'branch A'").
			OnBranch("main")

		worktreeA := sh.GetWorktreePath("branchA")
		shW := sh.InWorktree(worktreeA)
		shW.OnBranch("branchA").
			WriteFile("b.txt", "b content").
			Run("create branchB -m 'branch B'").
			OnBranch("branchB").
			WriteFile("c.txt", "c content").
			Run("create branchC -m 'branch C'").
			OnBranch("branchC")

		// Verify initial stack structure
		sh.Log("Verifying initial stack structure...")
		sh.ExpectBranchParent("branchA", "main")
		sh.ExpectBranchParent("branchB", "branchA")
		sh.ExpectBranchParent("branchC", "branchB")

		// Simulate A getting merged on GitHub
		sh.Log("Simulating branchA merge on GitHub...")
		sh.Git("checkout main").
			Git("merge branchA --ff-only").
			Git("push origin main")

		// Mark branchA PR as merged
		sh.SetPrState("branchA", "MERGED")

		// Run sync from main repo
		sh.Log("Running sync from main repo...")
		sh.OnBranch("main").
			Run("sync")

		// Verify branchA is deleted (check git branch --list returns empty)
		sh.Log("Verifying branchA is deleted...")
		sh.Git("branch --list branchA")
		if sh.Output() != "" {
			t.Errorf("branchA should have been deleted, but still exists")
		}

		// Verify B is reparented to main
		sh.Log("Verifying B is reparented to main...")
		sh.ExpectBranchParent("branchB", "main")
		sh.ExpectBranchParent("branchC", "branchB")

		// Verify worktree still exists and is functional
		sh.Log("Verifying worktree still exists...")
		shW.Git("status --porcelain") // This would fail if worktree doesn't exist

		// The worktree should now have branchB checked out (or be on branchC)
		// Since we were on branchC, we should still be on branchC
		shW.OnBranch("branchC")

		// Verify we can still work in the worktree
		sh.Log("Verifying worktree is still functional...")
		shW.WriteFile("new.txt", "new content").
			Run("create branchD -m 'branch D'").
			OnBranch("branchD")

		sh.ExpectBranchParent("branchD", "branchC")
	})

	t.Run("forked stack in worktree - bottom branch merged", func(t *testing.T) {
		// Test scenario: forked stack where A has two children B and C
		// Structure: main -> A -> B
		//                    \-> C
		// When A is merged:
		// - B and C both get reparented to main (become independent stacks)
		// - Worktree should survive and track the currently checked-out branch
		// - Both B and C should still be accessible from the worktree

		sh := NewTestShellWithRemoteInProcess(t)

		// Create forked stack in a worktree
		sh.Log("Creating forked stack A -> B, A -> C in worktree...")
		sh.WriteFile("a.txt", "a content").
			Run("create branchA -w -m 'branch A'").
			OnBranch("main")

		worktreeA := sh.GetWorktreePath("branchA")
		shW := sh.InWorktree(worktreeA)

		// Create first child B from A
		shW.OnBranch("branchA").
			WriteFile("b.txt", "b content").
			Run("create branchB -m 'branch B'").
			OnBranch("branchB")

		// Go back to A and create second child C (creating the fork)
		shW.Checkout("branchA").
			WriteFile("c.txt", "c content").
			Run("create branchC -m 'branch C'").
			OnBranch("branchC")

		// Verify forked stack structure
		sh.Log("Verifying forked stack structure...")
		sh.ExpectBranchParent("branchA", "main")
		sh.ExpectBranchParent("branchB", "branchA")
		sh.ExpectBranchParent("branchC", "branchA")

		// Simulate A getting merged on GitHub
		sh.Log("Simulating branchA merge on GitHub...")
		sh.Git("checkout main").
			Git("merge branchA --ff-only").
			Git("push origin main")

		// Mark branchA PR as merged
		sh.SetPrState("branchA", "MERGED")

		// Run sync from main repo
		sh.Log("Running sync from main repo...")
		sh.OnBranch("main").
			Run("sync")

		// Verify branchA is deleted
		sh.Log("Verifying branchA is deleted...")
		sh.Git("branch --list branchA")
		if sh.Output() != "" {
			t.Errorf("branchA should have been deleted, but still exists")
		}

		// Both B and C should be reparented to main (independent stacks now)
		sh.Log("Verifying B and C are reparented to main...")
		sh.ExpectBranchParent("branchB", "main")
		sh.ExpectBranchParent("branchC", "main")

		// Worktree should still exist and be functional
		sh.Log("Verifying worktree still exists...")
		shW.Git("status --porcelain") // This would fail if worktree doesn't exist

		// We were on branchC, should still be on branchC
		shW.OnBranch("branchC")

		// Verify both branches are accessible from the worktree
		sh.Log("Verifying both branches are accessible...")
		shW.Checkout("branchB").
			OnBranch("branchB")
		shW.Checkout("branchC").
			OnBranch("branchC")

		// Verify we can continue working on both stacks
		sh.Log("Verifying we can work on branchB stack...")
		shW.Checkout("branchB").
			WriteFile("b-child.txt", "b child content").
			Run("create branchB-child -m 'B child'").
			OnBranch("branchB-child")

		sh.ExpectBranchParent("branchB-child", "branchB")

		sh.Log("Verifying we can work on branchC stack...")
		shW.Checkout("branchC").
			WriteFile("c-child.txt", "c child content").
			Run("create branchC-child -m 'C child'").
			OnBranch("branchC-child")

		sh.ExpectBranchParent("branchC-child", "branchC")
	})
}
