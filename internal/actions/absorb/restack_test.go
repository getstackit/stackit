package absorb

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

func TestSelectRestackBranches(t *testing.T) {
	// Not parallel: subtests share a single scenario and one mutates scope.
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
		WithStack(map[string]string{
			"a": "main",
			"b": "a",
			"c": "b",
			"d": "a",
		})

	graph := engine.BuildStackGraph(s.Engine, engine.SortStrategyAlphabetical, nil)

	t.Run("all mode uses descendants of oldest modified branch", func(t *testing.T) {
		branches := selectRestackBranches(
			graph,
			s.Engine,
			RestackAll,
			"b",
			"a",
			engine.Scope{},
		)
		require.Equal(t, []string{"b", "c", "d"}, branchNames(branches))
	})

	t.Run("current mode excludes current when oldest modified is current", func(t *testing.T) {
		branches := selectRestackBranches(
			graph,
			s.Engine,
			RestackCurrent,
			"b",
			"b",
			engine.Scope{},
		)
		require.Equal(t, []string{"c"}, branchNames(branches))
	})

	t.Run("current mode includes current subtree when oldest modified is downstack", func(t *testing.T) {
		branches := selectRestackBranches(
			graph,
			s.Engine,
			RestackCurrent,
			"b",
			"a",
			engine.Scope{},
		)
		require.Equal(t, []string{"b", "c"}, branchNames(branches))
	})

	t.Run("scope mode filters branches outside current scope", func(t *testing.T) {
		err := s.Engine.SetScope(context.Background(), s.Engine.GetBranch("a"), engine.NewScope("proj"))
		require.NoError(t, err)
		err = s.Engine.SetScope(context.Background(), s.Engine.GetBranch("b"), engine.NewScope("proj"))
		require.NoError(t, err)
		err = s.Engine.SetScope(context.Background(), s.Engine.GetBranch("c"), engine.NewScope("proj"))
		require.NoError(t, err)
		err = s.Engine.SetScope(context.Background(), s.Engine.GetBranch("d"), engine.NewScope("other"))
		require.NoError(t, err)
		s.Rebuild()

		refreshedGraph := engine.BuildStackGraph(s.Engine, engine.SortStrategyAlphabetical, nil)
		branches := selectRestackBranches(
			refreshedGraph,
			s.Engine,
			RestackScope,
			"b",
			"a",
			engine.NewScope("proj"),
		)
		require.Equal(t, []string{"b", "c"}, branchNames(branches))
	})

	t.Run("scope mode falls back to current mode when current scope is empty", func(t *testing.T) {
		branches := selectRestackBranches(
			graph,
			s.Engine,
			RestackScope,
			"b",
			"a",
			engine.Scope{},
		)
		require.Equal(t, []string{"b", "c"}, branchNames(branches))
	})

	t.Run("none mode returns no branches", func(t *testing.T) {
		branches := selectRestackBranches(
			graph,
			s.Engine,
			RestackNone,
			"b",
			"a",
			engine.Scope{},
		)
		require.Empty(t, branches)
	})
}

func TestRestackModeValidate(t *testing.T) {
	t.Parallel()
	require.NoError(t, RestackAll.Validate())
	require.NoError(t, RestackCurrent.Validate())
	require.NoError(t, RestackScope.Validate())
	require.NoError(t, RestackNone.Validate())
	require.Error(t, RestackMode("bad").Validate())
}

func branchNames(branches []engine.Branch) []string {
	names := make([]string, 0, len(branches))
	for _, branch := range branches {
		names = append(names, branch.GetName())
	}
	return names
}
