package integration

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestRestackAppliesStacksAtomically locks in per-stack atomicity for
// non-interactive restacks: a conflict anywhere in an independent stack holds
// back the ENTIRE stack (even members that would rebase cleanly), while
// unrelated stacks restack fully.
//
// Without atomicity, restack would move b1 onto the new trunk but leave its
// conflicted child b2 behind, converting a benign "behind trunk" state into
// "child not restacked on parent" — which submit validation treats as a hard
// error. Holding the whole stack back keeps it internally consistent.
func TestRestackAppliesStacksAtomically(t *testing.T) {
	t.Parallel()
	sh := NewTestShellInProcess(t)

	// Seed a shared file on main that stack B will conflict on.
	sh.WriteFile("shared.txt", "base\n").
		Git("commit -m 'add shared file'")

	// Stack A (clean): main -> a1 -> a2, files disjoint from trunk's edit.
	sh.WriteFile("a1.txt", "a1\n").
		Run("create a1 -m 'a1'").
		OnBranch("a1")
	sh.WriteFile("a2.txt", "a2\n").
		Run("create a2 -m 'a2'").
		OnBranch("a2")

	// Stack B: main -> b1 (clean) -> b2 (rewrites the shared line).
	sh.Checkout("main")
	sh.WriteFile("b1.txt", "b1\n").
		Run("create b1 -m 'b1'").
		OnBranch("b1")
	sh.WriteFile("shared.txt", "base\nB2\n").
		Run("create b2 -m 'b2'").
		OnBranch("b2")

	// Trunk advances with an edit that conflicts with b2 only.
	sh.Checkout("main")
	sh.WriteFile("shared.txt", "base\nMAIN\n").
		Git("commit -m 'trunk edit on shared line'")

	mainRev := sh.revParse("main")
	b1Orig := sh.revParse("b1")
	b2Orig := sh.revParse("b2")

	// Every branch is accounted for in the output: restacked, conflicted, blocked.
	sh.Run("restack --continue-on-conflict").
		OutputContains("Restacked a1").
		OutputContains("Restacked a2").
		OutputContains("(conflict)").
		OutputContains("blocked by conflict in stack").
		OutputContains("restacked 2, skipped 1 (conflict), blocked 1")

	// Stack A restacked fully onto the new trunk.
	require.Equal(t, mainRev, sh.revParse("main"), "trunk itself must not move")
	require.Equal(t, mainRev, sh.revParse("a1^"), "a1 must sit directly on the new trunk tip")

	// Stack B is untouched end to end: the conflicted b2 AND its clean parent b1.
	require.Equal(t, b1Orig, sh.revParse("b1"),
		"b1 must be held back: applying it without its conflicted child would split the stack")
	require.Equal(t, b2Orig, sh.revParse("b2"), "conflicted b2 must not move")
}
