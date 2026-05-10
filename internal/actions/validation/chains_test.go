package validation_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/actions/validation"
	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

func TestModifyBranchChain(t *testing.T) {
	t.Parallel()

	t.Run("passes when on modifiable non-trunk branch", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithLinearStack3().Checkout("b")

		chain := validation.ModifyBranchChain(s.Engine, "rename")
		err := chain.Validate()
		require.NoError(t, err)
	})

	t.Run("fails when on trunk", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithLinearStack3().Checkout("main")

		chain := validation.ModifyBranchChain(s.Engine, "rename")
		err := chain.Validate()
		require.Error(t, err)
		require.Contains(t, err.Error(), "trunk")
	})
}

func TestGitOperationChain(t *testing.T) {
	t.Parallel()

	t.Run("passes when on tracked modifiable non-trunk branch with clean state", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithLinearStack3().Checkout("b")

		chain := validation.GitOperationChain(s.Context.Context, s.Engine, s.Context.Git(), "fold")
		err := chain.Validate()
		require.NoError(t, err)
	})

	t.Run("fails when on trunk", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithLinearStack3().Checkout("main")

		chain := validation.GitOperationChain(s.Context.Context, s.Engine, s.Context.Git(), "fold")
		err := chain.Validate()
		require.Error(t, err)
		require.Contains(t, err.Error(), "trunk")
	})

	t.Run("fails when has uncommitted changes", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithLinearStack3().Checkout("b").WithUncommittedChange("dirty")

		chain := validation.GitOperationChain(s.Context.Context, s.Engine, s.Context.Git(), "fold")
		err := chain.Validate()
		require.Error(t, err)
		require.Contains(t, err.Error(), "uncommitted")
	})
}

func TestAbsorbChain(t *testing.T) {
	t.Parallel()

	t.Run("passes when on modifiable non-trunk branch", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithLinearStack3().Checkout("b")

		chain := validation.AbsorbChain(s.Context.Context, s.Engine, s.Context.Git(), "absorb into")
		err := chain.Validate()
		require.NoError(t, err)
	})

	t.Run("fails when on trunk", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithLinearStack3().Checkout("main")

		chain := validation.AbsorbChain(s.Context.Context, s.Engine, s.Context.Git(), "absorb into")
		err := chain.Validate()
		require.Error(t, err)
		require.Contains(t, err.Error(), "trunk")
	})
}
