package integration

import (
	"testing"
)

func TestStackIDPreservation(t *testing.T) {
	t.Parallel()

	t.Run("restack preserves stack IDs", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		// Create a stack: main -> a -> b -> c
		sh.CreateLinearStack3()

		// Capture original stack ID
		originalStackID := sh.GetStackID("a")
		sh.ExpectStackIDsMatch("a", "b", "c")

		// Restack
		sh.Checkout("a")
		sh.Run("restack")

		// Verify stack IDs are unchanged
		sh.ExpectStackID("a", originalStackID)
		sh.ExpectStackIDsMatch("a", "b", "c")
	})

	t.Run("fold preserves stack IDs", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		// Create a stack: main -> a -> b -> c
		sh.CreateLinearStack3()

		// Capture original stack ID
		originalStackID := sh.GetStackID("a")
		sh.ExpectStackIDsMatch("a", "b", "c")

		// Fold b into a (keeping a)
		sh.Checkout("b")
		sh.Run("fold --keep current")

		// Verify remaining branches still have the same stack ID
		sh.ExpectStackID("b", originalStackID)
		sh.ExpectStackID("c", originalStackID)
	})

	t.Run("split preserves stack IDs via by-file", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		// Create a stack: main -> a (with 2 files in one commit)
		// Note: Write() creates files with _test.txt suffix (e.g., "file1" becomes "file1_test.txt")
		sh.Write("file1", "content 1").
			Write("file2", "content 2").
			Run("create a -m 'Add files'")

		// Capture original stack ID
		originalStackID := sh.GetStackID("a")

		// Split by extracting file2 to a new parent branch
		// The actual filename is file2_test.txt due to the test helper naming convention
		sh.Run("split --by-file file2_test.txt --name a-part1 -m 'Extract file2'")

		// Verify both branches have the same stack ID
		sh.ExpectStackID("a-part1", originalStackID)
		sh.ExpectStackID("a", originalStackID)
	})

	t.Run("pluck changes stack ID when moving to different stack", func(t *testing.T) {
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

		// Pluck b onto x (unlike move, pluck doesn't bring descendants)
		sh.Checkout("b")
		sh.Run("pluck --onto x --yes")

		// Verify b now has the second stack's ID
		sh.ExpectStackID("b", secondStackID)

		// Verify a still has the first stack ID
		sh.ExpectStackID("a", firstStackID)

		// Verify c is reparented to a and still has first stack ID
		sh.ExpectBranchParent("c", "a")
		sh.ExpectStackID("c", firstStackID)
	})

	t.Run("fold with siblings syncs stack IDs from new parent", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		// Create a diamond structure: main -> a -> [b, c]
		// where b and c are siblings (both children of a)
		sh.Write("a.txt", "content for a").
			Run("create a -m 'Add a'").
			Write("b.txt", "content for b").
			Run("create b -m 'Add b'").
			Checkout("a").
			Write("c.txt", "content for c").
			Run("create c -m 'Add c'")

		// Verify all have same stack ID
		originalStackID := sh.GetStackID("a")
		sh.ExpectStackIDsMatch("a", "b", "c")

		// Fold a into b (keeping b) - this makes b the new parent of c
		sh.Checkout("b")
		sh.Run("fold --keep current")

		// Verify b and c still share the same stack ID
		sh.ExpectStackID("b", originalStackID)
		sh.ExpectStackID("c", originalStackID)
	})
}
