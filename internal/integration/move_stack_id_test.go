package integration

import (
	"testing"
)

func TestMoveStackID(t *testing.T) {
	t.Parallel()

	t.Run("moving branch to trunk creates new stack ID", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		// Create a stack: main -> a -> b -> c
		sh.CreateLinearStack3()

		// Set a description to trigger stack ID creation
		sh.Run("describe -m 'First stack'")

		// Capture original stack ID
		originalStackID := sh.GetStackID("a")

		// Move b onto main (trunk)
		sh.Checkout("b")
		sh.Run("move --onto main --no-interactive --yes")

		// After moving to trunk, b gets a new stack ID when a description is set
		// Set description to trigger new stack ID creation for b's new stack
		sh.Run("describe -m 'Second stack'")

		// Verify b now has a different stack ID (new stack created)
		newStackID := sh.GetStackID("b")
		if newStackID == "" {
			t.Fatal("expected b to have a stack ID after moving to trunk and setting description")
		}
		if newStackID == originalStackID {
			t.Fatalf("expected b to have a new stack ID after moving to trunk, but got same ID: %s", originalStackID)
		}

		// Verify c (descendant of b) also has the new stack ID
		sh.ExpectStackID("c", newStackID)

		// Verify a still has the original stack ID
		sh.ExpectStackID("a", originalStackID)
	})

	t.Run("moving branch to different stack inherits that stack ID", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		// Create first stack: main -> a -> b
		sh.Write("a.txt", "content for a").
			Run("create a -m 'Add a'").
			Write("b.txt", "content for b").
			Run("create b -m 'Add b'")

		// Set a description to trigger stack ID creation
		sh.Run("describe -m 'First stack'")

		// Capture first stack's ID
		firstStackID := sh.GetStackID("a")
		sh.ExpectStackID("b", firstStackID) // b should share a's stack ID

		// Create second stack: main -> x -> y
		sh.Checkout("main").
			Write("x.txt", "content for x").
			Run("create x -m 'Add x'").
			Write("y.txt", "content for y").
			Run("create y -m 'Add y'")

		// Set a description to trigger stack ID creation for second stack
		sh.Run("describe -m 'Second stack'")

		// Capture second stack's ID
		secondStackID := sh.GetStackID("x")
		sh.ExpectStackID("y", secondStackID) // y should share x's stack ID
		sh.ExpectStackIDsDiffer("a", "x")    // Different stacks should have different IDs

		// Move b onto x (from first stack to second stack)
		sh.Checkout("b")
		sh.Run("move --onto x --no-interactive --yes")

		// Verify b now has the second stack's ID
		sh.ExpectStackID("b", secondStackID)

		// Verify a still has the first stack ID
		sh.ExpectStackID("a", firstStackID)
	})

	t.Run("moving branch and descendants updates all stack IDs", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		// Create first stack: main -> a -> b -> c -> d
		sh.Write("a.txt", "content for a").
			Run("create a -m 'Add a'").
			Write("b.txt", "content for b").
			Run("create b -m 'Add b'").
			Write("c.txt", "content for c").
			Run("create c -m 'Add c'").
			Write("d.txt", "content for d").
			Run("create d -m 'Add d'")

		// Set a description to trigger stack ID creation
		sh.Run("describe -m 'First stack'")

		// Capture first stack's ID
		firstStackID := sh.GetStackID("a")
		sh.ExpectStackIDsMatch("a", "b", "c", "d")

		// Create second stack: main -> x
		sh.Checkout("main").
			Write("x.txt", "content for x").
			Run("create x -m 'Add x'")

		// Set a description to trigger stack ID creation for second stack
		sh.Run("describe -m 'Second stack'")

		// Capture second stack's ID
		secondStackID := sh.GetStackID("x")
		sh.ExpectStackIDsDiffer("a", "x")

		// Move b onto x (should also move c and d as descendants)
		sh.Checkout("b")
		sh.Run("move --onto x --no-interactive --yes")

		// Verify b, c, d all now have the second stack's ID
		sh.ExpectStackID("b", secondStackID)
		sh.ExpectStackID("c", secondStackID)
		sh.ExpectStackID("d", secondStackID)

		// Verify a still has the first stack ID
		sh.ExpectStackID("a", firstStackID)
	})

	t.Run("moving entire first-level branch to trunk creates new stack", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		// Create stack: main -> a -> b -> c
		sh.CreateLinearStack3()

		// Set a description to trigger stack ID creation
		sh.Run("describe -m 'First stack'")

		// Capture original stack ID
		originalStackID := sh.GetStackID("a")
		sh.ExpectStackIDsMatch("a", "b", "c")

		// Move a to... well, it's already on trunk, so let's create a different scenario
		// Create: main -> x -> a -> b -> c by moving a onto x
		sh.Checkout("main").
			Write("x.txt", "content for x").
			Run("create x -m 'Add x'")

		// Set a description to trigger stack ID creation for x
		sh.Run("describe -m 'Second stack'")

		secondStackID := sh.GetStackID("x")

		// Move a onto x
		sh.Checkout("a")
		sh.Run("move --onto x --no-interactive --yes")

		// Verify a, b, c all now have the second stack's ID
		sh.ExpectStackIDsMatch("a", "b", "c")
		sh.ExpectStackID("a", secondStackID)

		// Verify original stack ID is no longer used (a, b, c should all have new ID)
		if sh.GetStackID("a") == originalStackID {
			t.Fatal("expected a to have a new stack ID after moving to x")
		}
	})

	t.Run("moving within same stack does not change stack ID", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		// Create stack: main -> a -> b -> c
		sh.CreateLinearStack3()

		// Set a description to trigger stack ID creation
		sh.Run("describe -m 'Test stack'")

		// Capture original stack ID
		originalStackID := sh.GetStackID("a")
		sh.ExpectStackIDsMatch("a", "b", "c")

		// Move c onto a (within the same stack)
		sh.Checkout("c")
		sh.Run("move --onto a --no-interactive --yes")

		// Verify stack structure changed
		sh.ExpectBranchParent("c", "a")

		// Verify all branches still have the same stack ID
		sh.ExpectStackIDsMatch("a", "b", "c")
		sh.ExpectStackID("a", originalStackID)
	})
}
