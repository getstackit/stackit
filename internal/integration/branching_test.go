package integration

import (
	"testing"
)

func TestBranchingStackSquash(t *testing.T) {
	t.Parallel()
	sh := NewTestShellInProcess(t)

	// Create a branching stack:
	//        main
	//          |
	//       branch-a (2 commits)
	//       /      \
	//   branch-b  branch-c
	//               |
	//            branch-d

	sh.Log("Creating branching stack structure...")
	sh.Write("a1", "content a1").Run("create branch-a -m 'feat: a1'")
	sh.Write("a2", "content a2").Run("modify -c -m 'feat: a2'") // branch-a now has 2 commits

	sh.Write("b1", "content b1").Run("create branch-b -m 'feat: b1'")

	sh.Checkout("branch-a")
	sh.Write("c1", "content c1").Run("create branch-c -m 'feat: c1'")

	sh.Write("d1", "content d1").Run("create branch-d -m 'feat: d1'")

	// Verify initial state
	sh.Log("Verifying initial state...")
	sh.Checkout("branch-a").CommitCount("main", "branch-a", 2)
	sh.Checkout("branch-b").CommitCount("branch-a", "branch-b", 1)
	sh.Checkout("branch-c").CommitCount("branch-a", "branch-c", 1)
	sh.Checkout("branch-d").CommitCount("branch-c", "branch-d", 1)

	// Squash branch-a
	sh.Log("Squashing branch-a...")
	sh.Checkout("branch-a").Run("squash --no-edit")
	sh.CommitCount("main", "branch-a", 1)

	// Verify all children were restacked and are still valid
	sh.Log("Verifying all children were restacked...")

	// Check branch-b
	sh.Checkout("branch-b").Run("info").OutputContains("branch-b")
	sh.Git("log -1 --format=%s").OutputContains("feat: b1")

	// Check branch-c
	sh.Checkout("branch-c").Run("info").OutputContains("branch-c")
	sh.Git("log -1 --format=%s").OutputContains("feat: c1")

	// Check branch-d
	sh.Checkout("branch-d").Run("info").OutputContains("branch-d")
	sh.Git("log -1 --format=%s").OutputContains("feat: d1")

	sh.Log("✓ Branching stack squash complete!")
}

func TestBranchingStackMove(t *testing.T) {
	t.Parallel()
	sh := NewTestShellInProcess(t)

	// Structure:
	// main
	//   |-- scopes
	//   |     |-- tests
	//   |           |-- scope-on-create
	//   |-- st-alias

	sh.Log("Creating initial stack structure...")
	sh.Write("scopes", "scopes").Run("create scopes -m 'feat: scopes'")
	sh.Write("tests", "tests").Run("create tests -m 'feat: tests'")
	sh.Write("soc", "soc").Run("create scope-on-create -m 'feat: scope-on-create'")

	sh.Checkout("main")
	sh.Write("st-alias", "st-alias").Run("create st-alias -m 'feat: st-alias'")

	// Now move st-alias onto scopes
	sh.Log("Moving st-alias onto scopes...")
	sh.Run("move --onto scopes")

	// Verify st-alias is now a child of scopes
	sh.Run("info").OutputContains("scopes")

	// Verify scopes now has two children: tests and st-alias
	sh.Checkout("scopes").Run("info").OutputContains("tests").OutputContains("st-alias")

	// Now squash scopes
	sh.Log("Squashing scopes...")
	sh.Run("squash --no-edit")

	// Verify both children (tests and st-alias) were restacked
	sh.Log("Verifying restack of both children...")
	sh.Checkout("tests").Run("info").OutputContains("scopes")
	sh.Checkout("st-alias").Run("info").OutputContains("scopes")

	sh.Log("✓ Move into branching stack complete!")
}

func TestBranchingStackModify(t *testing.T) {
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
}
func TestMoveIntoBranchingStackComplex(t *testing.T) {
	t.Parallel()
	sh := NewTestShellInProcess(t)

	// Structure:
	// main
	//   |-- scopes
	//   |     |-- tests
	//   |           |-- scope-on-create
	//   |-- st-alias

	sh.Log("Creating initial stack structure...")
	sh.Write("scopes", "scopes").Run("create scopes -m 'feat: scopes'")
	sh.Write("tests", "tests").Run("create tests -m 'feat: tests'")
	sh.Write("soc", "soc").Run("create scope-on-create -m 'feat: scope-on-create'")

	sh.Checkout("main")
	sh.Write("st-alias", "st-alias").Run("create st-alias -m 'feat: st-alias'")

	// Now move st-alias onto scopes
	sh.Log("Moving st-alias onto scopes...")
	sh.Run("move --onto scopes")

	// Verify st-alias is now a child of scopes
	sh.Run("info").OutputContains("scopes")

	// Verify scopes now has two children: tests and st-alias
	sh.Checkout("scopes").Run("info").OutputContains("tests").OutputContains("st-alias")

	// Now squash scopes
	sh.Log("Squashing scopes...")
	sh.Run("squash --no-edit")

	// Verify both children (tests and st-alias) were restacked
	sh.Log("Verifying restack of both children...")
	sh.Checkout("tests").Run("info").OutputContains("scopes")
	sh.Checkout("st-alias").Run("info").OutputContains("scopes")

	sh.Log("✓ Move into branching stack complete!")
}
