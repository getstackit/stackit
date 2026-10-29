package engine_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

func TestStackRangeUpstack(t *testing.T) {
	t.Parallel()

	t.Run("include current", func(t *testing.T) {
		t.Parallel()
		rng := engine.StackRangeUpstack(engine.IncludeCurrentBranch)
		require.True(t, rng.RecursiveChildren)
		require.True(t, rng.IncludeCurrent)
		require.False(t, rng.RecursiveParents)
	})

	t.Run("exclude current", func(t *testing.T) {
		t.Parallel()
		rng := engine.StackRangeUpstack(engine.ExcludeCurrentBranch)
		require.True(t, rng.RecursiveChildren)
		require.False(t, rng.IncludeCurrent)
		require.False(t, rng.RecursiveParents)
	})
}

func TestStackRangeDownstack(t *testing.T) {
	t.Parallel()

	t.Run("include current", func(t *testing.T) {
		t.Parallel()
		rng := engine.StackRangeDownstack(engine.IncludeCurrentBranch)
		require.True(t, rng.RecursiveParents)
		require.True(t, rng.IncludeCurrent)
		require.False(t, rng.RecursiveChildren)
	})

	t.Run("exclude current", func(t *testing.T) {
		t.Parallel()
		rng := engine.StackRangeDownstack(engine.ExcludeCurrentBranch)
		require.True(t, rng.RecursiveParents)
		require.False(t, rng.IncludeCurrent)
		require.False(t, rng.RecursiveChildren)
	})
}

func TestStackRangeFull(t *testing.T) {
	t.Parallel()

	rng := engine.StackRangeFull()
	require.True(t, rng.RecursiveParents)
	require.True(t, rng.IncludeCurrent)
	require.True(t, rng.RecursiveChildren)
}

// TestStackGraphTraversal covers Upstack, Downstack, and FullStack on one
// linear stack (main -> a -> b -> c). The traversals only read the graph, so
// every case shares a single scenario and runs sequentially.
func TestStackGraphTraversal(t *testing.T) { //nolint:tparallel // subtests share one fixture and run in order
	t.Parallel()
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
	s.WithLinearStack3()

	graph := engine.BuildStackGraph(t.Context(), s.Engine, engine.SortStrategyAlphabetical, nil)

	t.Run("Upstack returns children with include current", func(t *testing.T) {
		names := graph.Upstack(s.Engine.GetBranch("a"), engine.IncludeCurrentBranch).Names()
		require.Contains(t, names, "a")
		require.Contains(t, names, "b")
		require.Contains(t, names, "c")
	})

	t.Run("Upstack returns children without current", func(t *testing.T) {
		names := graph.Upstack(s.Engine.GetBranch("a"), engine.ExcludeCurrentBranch).Names()
		require.NotContains(t, names, "a")
		require.Contains(t, names, "b")
		require.Contains(t, names, "c")
	})

	t.Run("Downstack returns parents with include current", func(t *testing.T) {
		names := graph.Downstack(s.Engine.GetBranch("c"), engine.IncludeCurrentBranch).Names()
		require.Contains(t, names, "a")
		require.Contains(t, names, "b")
		require.Contains(t, names, "c")
		// Trunk is excluded by Range
		require.NotContains(t, names, "main")
	})

	t.Run("Downstack returns parents without current", func(t *testing.T) {
		names := graph.Downstack(s.Engine.GetBranch("c"), engine.ExcludeCurrentBranch).Names()
		require.Contains(t, names, "a")
		require.Contains(t, names, "b")
		require.NotContains(t, names, "c")
	})

	t.Run("FullStack returns all branches in stack", func(t *testing.T) {
		names := graph.FullStack(s.Engine.GetBranch("b")).Names()
		require.Contains(t, names, "a")
		require.Contains(t, names, "b")
		require.Contains(t, names, "c")
	})
}
