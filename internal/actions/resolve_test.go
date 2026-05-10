package actions_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/actions"
	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

func TestResolveBranchName(t *testing.T) {
	t.Parallel()

	t.Run("returns provided branch name when not empty", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

		name, err := actions.ResolveBranchName(s.Engine, "feature")
		require.NoError(t, err)
		require.Equal(t, "feature", name)
	})

	t.Run("returns current branch when empty and on branch", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithLinearStack3().Checkout("b")

		name, err := actions.ResolveBranchName(s.Engine, "")
		require.NoError(t, err)
		require.Equal(t, "b", name)
	})
}

func TestResolveBranch(t *testing.T) {
	t.Parallel()

	t.Run("returns branch for provided name", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithLinearStack3()

		branch, err := actions.ResolveBranch(s.Engine, "b")
		require.NoError(t, err)
		require.Equal(t, "b", branch.GetName())
	})

	t.Run("returns current branch when empty", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithLinearStack3().Checkout("c")

		branch, err := actions.ResolveBranch(s.Engine, "")
		require.NoError(t, err)
		require.Equal(t, "c", branch.GetName())
	})
}
