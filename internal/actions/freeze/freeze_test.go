package freeze_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/actions/freeze"
	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

func deps(s *scenario.Scenario) freeze.Deps {
	return freeze.Deps{Engine: s.Context.Engine, Out: s.Context.Output}
}

func TestFreezeAndUnfreeze(t *testing.T) {
	t.Parallel()

	t.Run("freeze covers the branch and its downstack only", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).WithLinearStack3()

		require.NoError(t, freeze.Freeze(t.Context(), deps(s), "b"))

		require.True(t, s.Engine.GetBranch("a").IsFrozen())
		require.True(t, s.Engine.GetBranch("b").IsFrozen())
		require.False(t, s.Engine.GetBranch("c").IsFrozen())
	})

	t.Run("unfreeze covers the branch and its upstack only", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).WithLinearStack3()
		require.NoError(t, freeze.Freeze(t.Context(), deps(s), "c"))

		require.NoError(t, freeze.Unfreeze(t.Context(), deps(s), "b"))

		require.True(t, s.Engine.GetBranch("a").IsFrozen())
		require.False(t, s.Engine.GetBranch("b").IsFrozen())
		require.False(t, s.Engine.GetBranch("c").IsFrozen())
	})

	t.Run("refuses trunk and untracked branches", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).WithLinearStack3()

		require.ErrorContains(t, freeze.Freeze(t.Context(), deps(s), "main"), "cannot freeze trunk")
		require.ErrorContains(t, freeze.Unfreeze(t.Context(), deps(s), "missing"), "not tracked")
	})
}
