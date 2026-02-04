package integration

import (
	"testing"
)

func TestPluckStackID(t *testing.T) {
	t.Parallel()

	t.Run("plucking branch to trunk creates new stack ID", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		// Create a stack: main -> a -> b -> c
		sh.CreateLinearStack3()

		// Capture original stack ID
		originalStackID := sh.GetStackID("a")
		sh.ExpectStackIDsMatch("a", "b", "c")

		// Pluck b onto main (trunk) - unlike move, children stay with grandparent
		sh.Checkout("b")
		sh.Run("pluck --onto main --yes")

		// Verify b now has a different stack ID (new stack created)
		newStackID := sh.GetStackID("b")
		if newStackID == "" {
			t.Fatal("expected b to have a stack ID after plucking to trunk")
		}
		if newStackID == originalStackID {
			t.Fatalf("expected b to have a new stack ID after plucking to trunk, but got same ID: %s", originalStackID)
		}

		// Verify c (was reparented to a, not moved with b) still has original stack ID
		sh.ExpectStackID("c", originalStackID)

		// Verify a still has the original stack ID
		sh.ExpectStackID("a", originalStackID)

		// Verify b is now on trunk (its own stack root)
		sh.ExpectBranchParent("b", "main")
	})

	t.Run("plucking branch to different stack inherits that stack ID", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		// Create first stack: main -> a -> b -> c
		sh.Write("a.txt", "content for a").
			Run("create a -m 'Add a'").
			Write("b.txt", "content for b").
			Run("create b -m 'Add b'").
			Write("c.txt", "content for c").
			Run("create c -m 'Add c'")

		// Capture first stack's ID
		firstStackID := sh.GetStackID("a")
		sh.ExpectStackIDsMatch("a", "b", "c")

		// Create second stack: main -> x
		sh.Checkout("main").
			Write("x.txt", "content for x").
			Run("create x -m 'Add x'")

		// Capture second stack's ID
		secondStackID := sh.GetStackID("x")
		sh.ExpectStackIDsDiffer("a", "x")

		// Pluck b onto x (from first stack to second stack)
		// c should be reparented to a (stays in first stack)
		sh.Checkout("b")
		sh.Run("pluck --onto x --yes")

		// Verify b now has the second stack's ID
		sh.ExpectStackID("b", secondStackID)

		// Verify a and c still have the first stack ID
		sh.ExpectStackID("a", firstStackID)
		sh.ExpectStackID("c", firstStackID)

		// Verify c was reparented to a (not moved with b)
		sh.ExpectBranchParent("c", "a")
	})

	t.Run("plucking within same stack does not change stack ID", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		// Create stack: main -> a -> b -> c -> d
		sh.Write("a.txt", "content for a").
			Run("create a -m 'Add a'").
			Write("b.txt", "content for b").
			Run("create b -m 'Add b'").
			Write("c.txt", "content for c").
			Run("create c -m 'Add c'").
			Write("d.txt", "content for d").
			Run("create d -m 'Add d'")

		// Capture original stack ID
		originalStackID := sh.GetStackID("a")
		sh.ExpectStackIDsMatch("a", "b", "c", "d")

		// Pluck c onto a (within the same stack)
		// d should be reparented to b
		sh.Checkout("c")
		sh.Run("pluck --onto a --yes")

		// Verify all branches still have the same stack ID
		sh.ExpectStackIDsMatch("a", "b", "c", "d")
		sh.ExpectStackID("a", originalStackID)

		// Verify c is now on a
		sh.ExpectBranchParent("c", "a")

		// Verify d was reparented to b
		sh.ExpectBranchParent("d", "b")
	})

	t.Run("plucking leaf branch to trunk creates new stack", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		// Create stack: main -> a -> b -> c
		sh.CreateLinearStack3()

		// Capture original stack ID
		originalStackID := sh.GetStackID("a")
		sh.ExpectStackIDsMatch("a", "b", "c")

		// Pluck c (leaf) onto trunk
		sh.Checkout("c")
		sh.Run("pluck --onto main --yes")

		// Verify c now has a new stack ID
		newStackID := sh.GetStackID("c")
		if newStackID == "" {
			t.Fatal("expected c to have a stack ID after plucking to trunk")
		}
		if newStackID == originalStackID {
			t.Fatalf("expected c to have a new stack ID after plucking to trunk, but got same ID: %s", originalStackID)
		}

		// Verify a and b still have the original stack ID
		sh.ExpectStackID("a", originalStackID)
		sh.ExpectStackID("b", originalStackID)

		// Verify c is on trunk
		sh.ExpectBranchParent("c", "main")
	})
}
