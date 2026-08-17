package navigation

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

func TestTraverseUpwardSkipsWorktreeAnchors(t *testing.T) {
	t.Parallel()

	// Stack: main -> wt-anchor -> feature
	// "up" from main should land on "feature", skipping the anchor
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
		WithStack(map[string]string{
			"wt-anchor": "main",
			"feature":   "wt-anchor",
		})

	err := s.Engine.SetBranchType(s.Engine.GetBranch("wt-anchor"), git.BranchTypeWorktreeAnchor)
	require.NoError(t, err)

	graph := engine.BuildStackGraph(s.Engine, engine.SortStrategyAlphabetical, nil)
	target, err := traverseUpward("main", s.Context, graph, &NullHandler{})
	require.NoError(t, err)
	require.Equal(t, "feature", target)
}

func TestTraverseUpwardSkipsNestedWorktreeAnchors(t *testing.T) {
	t.Parallel()

	// Stack: main -> anchor1 -> anchor2 -> feature
	// "up" from main should land on "feature", skipping both anchors
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
		WithStack(map[string]string{
			"anchor1": "main",
			"anchor2": "anchor1",
			"feature": "anchor2",
		})

	err := s.Engine.SetBranchType(s.Engine.GetBranch("anchor1"), git.BranchTypeWorktreeAnchor)
	require.NoError(t, err)
	err = s.Engine.SetBranchType(s.Engine.GetBranch("anchor2"), git.BranchTypeWorktreeAnchor)
	require.NoError(t, err)

	graph := engine.BuildStackGraph(s.Engine, engine.SortStrategyAlphabetical, nil)
	target, err := traverseUpward("main", s.Context, graph, &NullHandler{})
	require.NoError(t, err)
	require.Equal(t, "feature", target)
}

func TestTraverseUpwardReturnsCurrentWhenOnlyChildIsAnchorWithNoDescendants(t *testing.T) {
	t.Parallel()

	// Stack: main -> wt-anchor (no children beyond anchor)
	// "up" from main should stay at main (anchor has no non-anchor children)
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
		WithStack(map[string]string{
			"wt-anchor": "main",
		})

	err := s.Engine.SetBranchType(s.Engine.GetBranch("wt-anchor"), git.BranchTypeWorktreeAnchor)
	require.NoError(t, err)

	graph := engine.BuildStackGraph(s.Engine, engine.SortStrategyAlphabetical, nil)
	target, err := traverseUpward("main", s.Context, graph, &NullHandler{})
	require.NoError(t, err)
	require.Equal(t, "main", target)
}

func TestTraverseDownwardSkipsWorktreeAnchorParent(t *testing.T) {
	t.Parallel()

	// Stack: main -> wt-anchor -> parent -> child
	// "down" from child should land on "parent", skipping the anchor
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
		WithStack(map[string]string{
			"wt-anchor": "main",
			"parent":    "wt-anchor",
			"child":     "parent",
		})

	err := s.Engine.SetBranchType(s.Engine.GetBranch("wt-anchor"), git.BranchTypeWorktreeAnchor)
	require.NoError(t, err)

	s.Checkout("child")
	target := traverseDownward("child", s.Context)
	require.Equal(t, "parent", target)
}

func TestTraverseDownwardSkipsNestedAnchorsToBottom(t *testing.T) {
	t.Parallel()

	// Stack: main -> anchor1 -> anchor2 -> feature
	// "down" from feature should stay at feature (parent chain is all anchors then trunk)
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
		WithStack(map[string]string{
			"anchor1": "main",
			"anchor2": "anchor1",
			"feature": "anchor2",
		})

	err := s.Engine.SetBranchType(s.Engine.GetBranch("anchor1"), git.BranchTypeWorktreeAnchor)
	require.NoError(t, err)
	err = s.Engine.SetBranchType(s.Engine.GetBranch("anchor2"), git.BranchTypeWorktreeAnchor)
	require.NoError(t, err)

	s.Checkout("feature")
	target := traverseDownward("feature", s.Context)
	// feature's parent chain is anchor2 -> anchor1 -> main (all anchors skip to trunk)
	// So feature IS the bottom-most non-anchor branch
	require.Equal(t, "feature", target)
}

func TestSwitchBranchActionTopSkipsWorktreeAnchors(t *testing.T) {
	t.Parallel()

	// Stack: main -> wt-anchor -> feature -> tip
	// "top" from main should land on "tip", skipping the anchor
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
		WithStack(map[string]string{
			"wt-anchor": "main",
			"feature":   "wt-anchor",
			"tip":       "feature",
		})

	err := s.Engine.SetBranchType(s.Engine.GetBranch("wt-anchor"), git.BranchTypeWorktreeAnchor)
	require.NoError(t, err)

	s.Checkout("main")
	result, err := SwitchBranchAction(DirectionTop, s.Context, &NullHandler{})
	require.NoError(t, err)
	// Should successfully target "tip" without erroring on the anchor
	require.Empty(t, result.WorktreeSwitchPath)
}

func TestSwitchBranchActionBottomSkipsWorktreeAnchors(t *testing.T) {
	t.Parallel()

	// Stack: main -> wt-anchor -> feature -> tip
	// "bottom" from tip should land on "feature", skipping the anchor
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
		WithStack(map[string]string{
			"wt-anchor": "main",
			"feature":   "wt-anchor",
			"tip":       "feature",
		})

	err := s.Engine.SetBranchType(s.Engine.GetBranch("wt-anchor"), git.BranchTypeWorktreeAnchor)
	require.NoError(t, err)

	s.Checkout("tip")
	result, err := SwitchBranchAction(DirectionBottom, s.Context, &NullHandler{})
	require.NoError(t, err)
	require.Empty(t, result.WorktreeSwitchPath)
}

func TestFlattenThroughAnchorsPromotesGrandchildren(t *testing.T) {
	t.Parallel()

	// Stack: main -> anchor -> [child1, child2]
	// FlattenThroughAnchors should replace anchor with child1 and child2
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
		WithStack(map[string]string{
			"anchor": "main",
			"child1": "anchor",
			"child2": "anchor",
		})

	err := s.Engine.SetBranchType(s.Engine.GetBranch("anchor"), git.BranchTypeWorktreeAnchor)
	require.NoError(t, err)

	graph := engine.BuildStackGraph(s.Engine, engine.SortStrategyAlphabetical, nil)
	trunk := s.Engine.Trunk()
	children := graph.ChildBranches(trunk)

	flattened := graph.FlattenThroughAnchors(children)
	names := make([]string, len(flattened))
	for i, b := range flattened {
		names[i] = b.GetName()
	}
	require.ElementsMatch(t, []string{"child1", "child2"}, names)
}
