package integration

import (
	"testing"
)

// =============================================================================
// Conflict Resolution Integration Tests
//
// These tests cover scenarios where rebasing causes conflicts and the user
// must resolve them using `stackit continue`.
// =============================================================================

func TestConflictResolution(t *testing.T) {
	t.Parallel()

	t.Run("continue through cascading conflicts in stack", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		// 1. Build stack: main → branch-a → branch-b → branch-c
		//    where each branch modifies the SAME file (to create conflicts)
		sh.Log("Building stack with potential conflicts...")
		sh.WriteFile("common.txt", "initial content").
			Run("create branch-a -m 'Branch A change'").
			OnBranch("branch-a")

		sh.WriteFile("common.txt", "branch a content\nbranch b change").
			Run("create branch-b -m 'Branch B change'").
			OnBranch("branch-b")

		sh.WriteFile("common.txt", "branch a content\nbranch b content\nbranch c change").
			Run("create branch-c -m 'Branch C change'").
			OnBranch("branch-c")

		// 2. Add a new commit to main that modifies the same file
		sh.Log("Adding conflicting change to main...")
		sh.Checkout("main").
			WriteFile("common.txt", "main content").
			Git("commit -m 'Main change'")

		// 3. Run `stackit restack` from branch-a
		sh.Log("Starting restack from branch-a...")
		sh.Checkout("branch-a").
			RunExpectError("restack --upstack").
			OutputContains("conflict").
			OutputContains("branch-a")

		// 4. Verify: restack stops at branch-a with conflict
		sh.Log("Verifying conflict on branch-a...")
		sh.Git("status").OutputContains("rebase in progress")

		// 5. Resolve conflict, run `stackit continue`
		sh.Log("Resolving conflict on branch-a and continuing...")
		sh.WriteFile("common.txt", "main content\nbranch a content").
			RunExpectError("continue").
			OutputContains("conflict").
			OutputContains("branch-b")

		// 6. Verify: restack continues, stops at branch-b with conflict
		sh.Log("Verifying conflict on branch-b...")
		sh.Git("status").OutputContains("rebase in progress")

		// 7. Resolve conflict, run `stackit continue`
		sh.Log("Resolving conflict on branch-b and continuing...")
		sh.WriteFile("common.txt", "main content\nbranch a content\nbranch b content").
			RunExpectError("continue").
			OutputContains("conflict").
			OutputContains("branch-c")

		// 8. Verify: restack continues, stops at branch-c with conflict
		sh.Log("Verifying conflict on branch-c...")
		sh.Git("status").OutputContains("rebase in progress")

		// 9. Resolve conflict, run `stackit continue`
		sh.Log("Resolving conflict on branch-c and completing...")
		sh.WriteFile("common.txt", "main content\nbranch a content\nbranch b content\nbranch c content").
			Run("continue")

		// 10. Verify: all branches are now successfully restacked
		sh.Log("Verifying final stack state...")
		sh.Checkout("branch-c").
			Run("info").
			OutputContains("branch-c").
			OutputContains("Parent: branch-b")

		sh.Checkout("branch-b").
			Run("info").
			OutputContains("branch-b").
			OutputContains("Parent: branch-a")

		sh.Checkout("branch-a").
			Run("info").
			OutputContains("branch-a").
			OutputContains("Parent: main")

		sh.Log("✓ Cascading conflict resolution test complete!")
	})

	t.Run("continue preserves stack structure after mid-stack conflict", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		// 1. Build stack: main → a → b → c → d
		sh.Log("Building stack: main -> a -> b -> c -> d")
		sh.WriteFile("a.txt", "a").Run("create a -m 'a'")
		sh.WriteFile("b.txt", "b").Run("create b -m 'b'")
		sh.WriteFile("c.txt", "c").Run("create c -m 'c'")
		sh.WriteFile("d.txt", "d").Run("create d -m 'd'")

		// 2. Amend branch-b with conflicting changes for c
		sh.Log("Amending branch-b with conflicting change for branch-c")
		sh.Checkout("b")
		sh.WriteFile("c.txt", "conflict with c").
			Git("commit --amend --no-edit")

		// 3. Run `stackit restack --upstack` from branch-b
		sh.Log("Running restack --upstack from branch-b")
		sh.RunExpectError("restack --upstack").
			OutputContains("conflict").
			OutputContains("restacking c")

		// 4. Verify: conflict occurs at branch-c
		sh.Log("Verifying conflict on branch-c")
		sh.Git("status").OutputContains("rebase in progress")

		// 5. Resolve conflict, run continue
		sh.Log("Resolving conflict on branch-c and continuing")
		sh.WriteFile("c.txt", "conflict with c resolved").
			Run("continue")

		// 6. Verify: branch-c and branch-d are properly restacked
		sh.Log("Verifying branch-c and branch-d are restacked")
		sh.Checkout("d").Run("info").OutputContains("d").OutputContains("Parent: c")
		sh.Checkout("c").Run("info").OutputContains("c").OutputContains("Parent: b")

		// 7. Verify: parent relationships are preserved correctly
		sh.Log("Verifying stack integrity")
		sh.Checkout("b").Run("info").OutputContains("b").OutputContains("Parent: a")
		sh.Checkout("a").Run("info").OutputContains("a").OutputContains("Parent: main")

		sh.Log("✓ Mid-stack conflict resolution test complete!")
	})

	t.Run("continue handles branching stack conflict", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		// 1. Build branching stack: main → branch-a → [branch-b, branch-c]
		sh.Log("Building branching stack: main -> branch-a -> [branch-b, branch-c]")
		sh.WriteFile("common.txt", "base").
			Run("create branch-a -m 'a'").
			OnBranch("branch-a")

		sh.WriteFile("b.txt", "b").
			Run("create branch-b -m 'b'").
			OnBranch("branch-b")

		sh.Checkout("branch-a").
			WriteFile("c.txt", "c").
			Run("create branch-c -m 'c'").
			OnBranch("branch-c")

		// 2. Introduce conflict in branch-a
		sh.Log("Introducing conflict in branch-a...")
		sh.Checkout("main").
			WriteFile("common.txt", "main").
			Git("commit -m 'main'").
			Checkout("branch-a")

		// 3. Run restack --upstack from branch-a
		sh.Log("Running restack --upstack from branch-a...")
		sh.RunExpectError("restack --upstack").
			OutputContains("conflict").
			OutputContains("branch-a")

		// 4. Resolve conflict in branch-a
		sh.Log("Resolving conflict in branch-a...")
		sh.WriteFile("common.txt", "main\nplus a").
			Run("continue")

		// 5. Verify that both branch-b and branch-c were restacked
		sh.Log("Verifying branch-b and branch-c were restacked...")

		sh.Checkout("branch-b").
			Run("info").
			OutputContains("branch-b").
			OutputContains("Parent: branch-a")

		sh.Checkout("branch-c").
			Run("info").
			OutputContains("branch-c").
			OutputContains("Parent: branch-a")

		// Verify branch-a is parented to main
		sh.Checkout("branch-a").
			Run("info").
			OutputContains("branch-a").
			OutputContains("Parent: main")

		sh.Log("✓ Branching stack conflict resolution test complete!")
	})
}
