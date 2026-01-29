package engine_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"stackit.dev/stackit/internal/engine"
	"stackit.dev/stackit/testhelpers"
	"stackit.dev/stackit/testhelpers/scenario"
)

func TestStackGraphRangeAncestorsExcludeTrunk(t *testing.T) {
	t.Parallel()

	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
		WithStack(map[string]string{
			"branch1": "main",
			"branch2": "branch1",
		})

	graph := engine.BuildStackGraph(s.Engine, engine.SortStrategyAlphabetical, nil)
	branches := graph.Range(s.Engine.GetBranch("branch2"), engine.StackRange{RecursiveParents: true})

	names := make([]string, 0, len(branches))
	for _, b := range branches {
		names = append(names, b.GetName())
	}

	require.Equal(t, []string{"branch1"}, names)
	require.NotContains(t, names, "main")
}

func TestStackGraphRangeDescendantsOrderParentsFirst(t *testing.T) {
	t.Parallel()

	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
		WithStack(map[string]string{
			"stackA":       "main",
			"stackA-child": "stackA",
			"stackB":       "main",
			"stackB-child": "stackB",
		})

	graph := engine.BuildStackGraph(s.Engine, engine.SortStrategyAlphabetical, nil)
	branches := graph.Range(s.Engine.Trunk(), engine.StackRange{
		RecursiveChildren: true,
	})

	names := make([]string, 0, len(branches))
	for _, b := range branches {
		names = append(names, b.GetName())
	}

	require.Len(t, names, 4)
	require.Contains(t, names, "stackA")
	require.Contains(t, names, "stackA-child")
	require.Contains(t, names, "stackB")
	require.Contains(t, names, "stackB-child")

	stackAIdx := indexOfName(names, "stackA")
	stackAChildIdx := indexOfName(names, "stackA-child")
	stackBIdx := indexOfName(names, "stackB")
	stackBChildIdx := indexOfName(names, "stackB-child")

	require.Less(t, stackAIdx, stackAChildIdx, "stackA should come before stackA-child")
	require.Less(t, stackBIdx, stackBChildIdx, "stackB should come before stackB-child")
}

func TestStackGraphFilterPrunesSubtrees(t *testing.T) {
	t.Parallel()

	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
		WithStack(map[string]string{
			"a":  "main",
			"a1": "a",
			"b":  "main",
			"b1": "b",
		})

	graph := engine.BuildStackGraph(s.Engine, engine.SortStrategyAlphabetical, func(b engine.Branch) bool {
		return b.GetName() != "b"
	})

	branches := graph.Range(s.Engine.Trunk(), engine.StackRange{
		RecursiveChildren: true,
	})

	names := make([]string, 0, len(branches))
	for _, b := range branches {
		names = append(names, b.GetName())
	}

	require.Contains(t, names, "a")
	require.Contains(t, names, "a1")
	require.NotContains(t, names, "b")
	require.NotContains(t, names, "b1")
}

// indexOfName returns the index of item in slice, or -1 if not found
func indexOfName(slice []string, item string) int {
	for i, s := range slice {
		if s == item {
			return i
		}
	}
	return -1
}

func TestStackGraphIsLeaf(t *testing.T) {
	t.Parallel()

	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
		WithStack(map[string]string{
			"parent": "main",
			"child":  "parent",
		})

	graph := engine.BuildStackGraph(s.Engine, engine.SortStrategyAlphabetical, nil)

	t.Run("leaf branch returns true", func(t *testing.T) {
		t.Parallel()
		require.True(t, graph.IsLeaf(s.Engine.GetBranch("child")))
	})

	t.Run("non-leaf branch returns false", func(t *testing.T) {
		t.Parallel()
		require.False(t, graph.IsLeaf(s.Engine.GetBranch("parent")))
	})

	t.Run("trunk with children returns false", func(t *testing.T) {
		t.Parallel()
		require.False(t, graph.IsLeaf(s.Engine.Trunk()))
	})
}

func TestStackGraphCollectBranches(t *testing.T) {
	t.Parallel()

	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
		WithStack(map[string]string{
			"a":  "main",
			"a1": "a",
			"a2": "a",
			"b":  "main",
		})

	graph := engine.BuildStackGraph(s.Engine, engine.SortStrategyAlphabetical, nil)

	t.Run("collects all descendants depth-first", func(t *testing.T) {
		t.Parallel()
		branches := graph.CollectBranches(s.Engine.GetBranch("a"))
		names := branchNames(branches)

		require.Len(t, names, 3)
		require.Equal(t, "a", names[0], "root should be first")
		require.Contains(t, names, "a1")
		require.Contains(t, names, "a2")
	})

	t.Run("leaf branch returns only itself", func(t *testing.T) {
		t.Parallel()
		branches := graph.CollectBranches(s.Engine.GetBranch("b"))
		names := branchNames(branches)

		require.Equal(t, []string{"b"}, names)
	})

	t.Run("collects from trunk", func(t *testing.T) {
		t.Parallel()
		branches := graph.CollectBranches(s.Engine.Trunk())
		names := branchNames(branches)

		require.Len(t, names, 5)
		require.Equal(t, "main", names[0], "trunk should be first")
	})
}

func TestStackGraphIsRelated(t *testing.T) {
	t.Parallel()

	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
		WithStack(map[string]string{
			"a":  "main",
			"a1": "a",
			"b":  "main",
		})

	graph := engine.BuildStackGraph(s.Engine, engine.SortStrategyAlphabetical, nil)

	t.Run("parent and child are related", func(t *testing.T) {
		t.Parallel()
		require.True(t, graph.IsRelated(s.Engine.GetBranch("a"), s.Engine.GetBranch("a1")))
	})

	t.Run("child and parent are related (reverse)", func(t *testing.T) {
		t.Parallel()
		require.True(t, graph.IsRelated(s.Engine.GetBranch("a1"), s.Engine.GetBranch("a")))
	})

	t.Run("grandparent and grandchild are related", func(t *testing.T) {
		t.Parallel()
		require.True(t, graph.IsRelated(s.Engine.Trunk(), s.Engine.GetBranch("a1")))
	})

	t.Run("siblings are not related", func(t *testing.T) {
		t.Parallel()
		require.False(t, graph.IsRelated(s.Engine.GetBranch("a"), s.Engine.GetBranch("b")))
	})

	t.Run("cousins are not related", func(t *testing.T) {
		t.Parallel()
		require.False(t, graph.IsRelated(s.Engine.GetBranch("a1"), s.Engine.GetBranch("b")))
	})

	t.Run("same branch is related to itself", func(t *testing.T) {
		t.Parallel()
		require.True(t, graph.IsRelated(s.Engine.GetBranch("a"), s.Engine.GetBranch("a")))
	})
}

func branchNames(branches []engine.Branch) []string {
	names := make([]string, len(branches))
	for i, b := range branches {
		names[i] = b.GetName()
	}
	return names
}
