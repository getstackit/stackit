package integration

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/testhelpers"
)

// TestReparentDivergencePreservation tests that reparenting operations
// preserve correct divergence points, so children don't carry commits
// from their old parent after restacking.
func TestReparentDivergencePreservation(t *testing.T) {
	t.Parallel()

	t.Run("delete branch preserves child commit counts", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		// Create stack: main -> a -> b -> c
		// Each branch has exactly 1 commit relative to its parent
		sh.CreateLinearStack3()

		// Verify initial commit counts
		sh.CommitCount("main", "a", 1)
		sh.CommitCount("a", "b", 1)
		sh.CommitCount("b", "c", 1)

		// Delete b -- children (c) should be reparented to a
		sh.Run("delete b --force")

		// After restacking, c should have exactly 1 commit relative to a
		// (not 2, which would happen if divergence point was set too far back)
		sh.ExpectBranchParent("c", "a")
		sh.Checkout("c")
		sh.Run("restack")
		sh.CommitCount("a", "c", 1)
	})

	t.Run("linear stack deletes a merged middle branch", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)
		sh.Run("config set stack.shape linear").CreateLinearStack3()

		eng, err := engine.NewEngine(engine.Options{
			RepoRoot:     sh.Dir(),
			Trunk:        "main",
			LinearStacks: true,
		})
		require.NoError(t, err)
		require.NoError(t, eng.UpsertPrInfo(context.Background(), eng.GetBranch("b"), testhelpers.NewTestPrInfoMerged(1, "a")))

		sh.Run("delete b")
		sh.ExpectBranchParent("c", "a")
	})

	t.Run("delete branch with multiple children preserves commit counts", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		// Create branching stack:
		//   main -> a -> b -> [c, d]
		sh.Write("a.txt", "a content").
			Run("create a -m 'Add a'")
		sh.Write("b.txt", "b content").
			Run("create b -m 'Add b'")
		sh.Write("c.txt", "c content").
			Run("create c -m 'Add c'")
		sh.Checkout("b")
		sh.Write("d.txt", "d content").
			Run("create d -m 'Add d'")

		// Verify initial state
		sh.CommitCount("main", "a", 1)
		sh.CommitCount("a", "b", 1)
		sh.CommitCount("b", "c", 1)
		sh.CommitCount("b", "d", 1)

		// Delete b -- c and d should be reparented to a
		sh.Run("delete b --force")

		// After restacking, c and d should each have 1 commit relative to a
		sh.ExpectBranchParent("c", "a")
		sh.ExpectBranchParent("d", "a")
		sh.Checkout("c")
		sh.Run("restack")
		sh.CommitCount("a", "c", 1)
		sh.Checkout("d")
		sh.Run("restack")
		sh.CommitCount("a", "d", 1)
	})

	t.Run("fold keep reparents siblings with correct divergence", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		// Create diamond: main -> a -> [b, d]
		// Then b -> c
		//
		//   main -> a -> b -> c
		//             \-> d
		sh.Write("a.txt", "a content").
			Run("create a -m 'Add a'")
		sh.Write("b.txt", "b content").
			Run("create b -m 'Add b'")
		sh.Write("c.txt", "c content").
			Run("create c -m 'Add c'")
		sh.Checkout("a")
		sh.Write("d.txt", "d content").
			Run("create d -m 'Add d'")

		// Verify initial state
		sh.CommitCount("main", "a", 1)
		sh.CommitCount("a", "b", 1)
		sh.CommitCount("b", "c", 1)
		sh.CommitCount("a", "d", 1)

		// Fold a into b (keep current=b) -- d (sibling) should be reparented to b
		sh.Checkout("b")
		sh.Run("fold --keep current")

		// After fold+restack, d should have exactly 1 commit relative to b
		// (not carry a's commit too)
		sh.ExpectBranchParent("d", "b")
		sh.CommitCount("b", "d", 1)

		// c should still have exactly 1 commit relative to b
		sh.ExpectBranchParent("c", "b")
		sh.CommitCount("b", "c", 1)
	})
}
