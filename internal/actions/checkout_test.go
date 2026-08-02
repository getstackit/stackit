package actions_test

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/actions"
	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

func TestCheckoutActionWorktreeSwitchIncludesTargetBranch(t *testing.T) {
	t.Parallel()

	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
		WithStack(map[string]string{
			"feature": "main",
		})

	err := s.Engine.RegisterWorktree("feature", s.Context.RepoRoot)
	require.NoError(t, err)
	defer func() { _ = s.Engine.UnregisterWorktree(s.Context, "feature") }()

	s.Checkout("main")

	result, err := actions.CheckoutAction(s.Context, actions.CheckoutOptions{BranchName: "feature"}, nil)
	require.NoError(t, err)
	canonicalRepoRoot, err := filepath.EvalSymlinks(s.Context.RepoRoot)
	require.NoError(t, err)
	canonicalWorktreeSwitchPath, err := filepath.EvalSymlinks(result.WorktreeSwitchPath)
	require.NoError(t, err)
	require.Equal(t, canonicalRepoRoot, canonicalWorktreeSwitchPath)
	require.Equal(t, "feature", result.TargetBranch)
}
