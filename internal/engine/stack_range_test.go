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
		rng := engine.StackRangeUpstack(true)
		require.True(t, rng.RecursiveChildren)
		require.True(t, rng.IncludeCurrent)
		require.False(t, rng.RecursiveParents)
	})

	t.Run("exclude current", func(t *testing.T) {
		t.Parallel()
		rng := engine.StackRangeUpstack(false)
		require.True(t, rng.RecursiveChildren)
		require.False(t, rng.IncludeCurrent)
		require.False(t, rng.RecursiveParents)
	})
}

func TestStackRangeDownstack(t *testing.T) {
	t.Parallel()

	t.Run("include current", func(t *testing.T) {
		t.Parallel()
		rng := engine.StackRangeDownstack(true)
		require.True(t, rng.RecursiveParents)
		require.True(t, rng.IncludeCurrent)
		require.False(t, rng.RecursiveChildren)
	})

	t.Run("exclude current", func(t *testing.T) {
		t.Parallel()
		rng := engine.StackRangeDownstack(false)
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

func TestStackGraphUpstack(t *testing.T) {
	t.Parallel()

	t.Run("returns children with include current", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithLinearStack3()

		graph := engine.BuildStackGraph(s.Engine, engine.SortStrategyAlphabetical, nil)
		branches := graph.Upstack(s.Engine.GetBranch("a"), true)

		names := branches.Names()
		require.Contains(t, names, "a")
		require.Contains(t, names, "b")
		require.Contains(t, names, "c")
	})

	t.Run("returns children without current", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithLinearStack3()

		graph := engine.BuildStackGraph(s.Engine, engine.SortStrategyAlphabetical, nil)
		branches := graph.Upstack(s.Engine.GetBranch("a"), false)

		names := branches.Names()
		require.NotContains(t, names, "a")
		require.Contains(t, names, "b")
		require.Contains(t, names, "c")
	})
}

func TestStackGraphDownstack(t *testing.T) {
	t.Parallel()

	t.Run("returns parents with include current", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithLinearStack3()

		graph := engine.BuildStackGraph(s.Engine, engine.SortStrategyAlphabetical, nil)
		branches := graph.Downstack(s.Engine.GetBranch("c"), true)

		names := branches.Names()
		require.Contains(t, names, "a")
		require.Contains(t, names, "b")
		require.Contains(t, names, "c")
		// Trunk is excluded by Range
		require.NotContains(t, names, "main")
	})

	t.Run("returns parents without current", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithLinearStack3()

		graph := engine.BuildStackGraph(s.Engine, engine.SortStrategyAlphabetical, nil)
		branches := graph.Downstack(s.Engine.GetBranch("c"), false)

		names := branches.Names()
		require.Contains(t, names, "a")
		require.Contains(t, names, "b")
		require.NotContains(t, names, "c")
	})
}

func TestStackGraphFullStack(t *testing.T) {
	t.Parallel()

	t.Run("returns all branches in stack", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithLinearStack3()

		graph := engine.BuildStackGraph(s.Engine, engine.SortStrategyAlphabetical, nil)
		branches := graph.FullStack(s.Engine.GetBranch("b"))

		names := branches.Names()
		require.Contains(t, names, "a")
		require.Contains(t, names, "b")
		require.Contains(t, names, "c")
	})
}
