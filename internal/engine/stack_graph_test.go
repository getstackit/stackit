package engine_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
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

	require.Equal(t, []string{"branch1"}, branches.Names())
	require.False(t, branches.Contains("main"))
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

	names := branches.Names()

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

	names := branches.Names()

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
		names := branches.Names()

		require.Len(t, names, 3)
		require.Equal(t, "a", names[0], "root should be first")
		require.Contains(t, names, "a1")
		require.Contains(t, names, "a2")
	})

	t.Run("leaf branch returns only itself", func(t *testing.T) {
		t.Parallel()
		branches := graph.CollectBranches(s.Engine.GetBranch("b"))
		names := branches.Names()

		require.Equal(t, []string{"b"}, names)
	})

	t.Run("collects from trunk", func(t *testing.T) {
		t.Parallel()
		branches := graph.CollectBranches(s.Engine.Trunk())
		names := branches.Names()

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

func TestStackGraphForEachDepth(t *testing.T) {
	t.Parallel()

	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
		WithStack(map[string]string{
			"a":  "main",
			"b":  "main",
			"a1": "a",
			"b1": "b",
			"a2": "a1",
		})

	graph := engine.BuildStackGraph(s.Engine, engine.SortStrategyAlphabetical, nil)

	t.Run("iterates depths in order", func(t *testing.T) {
		t.Parallel()

		depthOrder := []int{}
		err := graph.ForEachDepth(func(depth int, _ engine.Branches) error {
			depthOrder = append(depthOrder, depth)
			return nil
		})
		require.NoError(t, err)
		require.Equal(t, []int{0, 1, 2, 3}, depthOrder)
	})

	t.Run("branches at same depth are independent", func(t *testing.T) {
		t.Parallel()

		branchesAtDepth1 := []string{}
		err := graph.ForEachDepth(func(depth int, branches engine.Branches) error {
			if depth == 1 {
				branchesAtDepth1 = branches.Names()
			}
			return nil
		})
		require.NoError(t, err)
		require.ElementsMatch(t, []string{"a", "b"}, branchesAtDepth1)
	})

	t.Run("stops on error", func(t *testing.T) {
		t.Parallel()

		testErr := errors.New("test error")
		callCount := 0
		err := graph.ForEachDepth(func(depth int, _ engine.Branches) error {
			callCount++
			if depth == 1 {
				return testErr
			}
			return nil
		})
		require.ErrorIs(t, err, testErr)
		require.Equal(t, 2, callCount) // depth 0 and 1
	})
}

func TestStackGraphMaxDepth(t *testing.T) {
	t.Parallel()

	t.Run("returns max depth", func(t *testing.T) {
		t.Parallel()

		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"a":  "main",
				"a1": "a",
				"a2": "a1",
			})

		graph := engine.BuildStackGraph(s.Engine, engine.SortStrategyAlphabetical, nil)
		require.Equal(t, 3, graph.MaxDepth())
	})

	t.Run("returns -1 for empty graph", func(t *testing.T) {
		t.Parallel()

		// Empty graph by filtering out everything
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		graph := engine.BuildStackGraph(s.Engine, engine.SortStrategyAlphabetical, func(engine.Branch) bool {
			return false
		})
		require.Equal(t, -1, graph.MaxDepth())
	})
}

func TestStackGraphBranchesAtDepth(t *testing.T) {
	t.Parallel()

	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
		WithStack(map[string]string{
			"a":  "main",
			"b":  "main",
			"a1": "a",
		})

	graph := engine.BuildStackGraph(s.Engine, engine.SortStrategyAlphabetical, nil)

	t.Run("returns branches at depth 1", func(t *testing.T) {
		t.Parallel()
		branches := graph.BranchesAtDepth(1)
		names := branches.Names()
		require.ElementsMatch(t, []string{"a", "b"}, names)
	})

	t.Run("returns nil for non-existent depth", func(t *testing.T) {
		t.Parallel()
		branches := graph.BranchesAtDepth(100)
		require.Nil(t, branches)
	})
}
