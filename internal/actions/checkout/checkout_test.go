package checkout_test

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/actions"
	"github.com/getstackit/stackit/internal/actions/checkout"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

func deps(s *scenario.Scenario) checkout.Deps {
	return checkout.Deps{
		Engine: s.Context.Engine,
		Out:    s.Context.Output,
		Log:    s.Context.Logger,
		Quiet:  s.Context.Quiet,
		Here:   actions.HereFrom(s.Context),
	}
}

func TestActionWorktreeSwitchIncludesTargetBranch(t *testing.T) {
	t.Parallel()

	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
		WithStack(map[string]string{
			"feature": "main",
		})

	err := s.Engine.RegisterWorktree(t.Context(), engine.WorktreeRegistration{AnchorBranch: "feature", Path: engine.WorktreePath(s.Context.RepoRoot)})
	require.NoError(t, err)
	defer func() { _ = s.Engine.UnregisterWorktree(s.Context, "feature") }()

	s.Checkout("main")

	result, err := checkout.Action(t.Context(), deps(s), checkout.Options{BranchName: "feature"}, nil)
	require.NoError(t, err)
	canonicalRepoRoot, err := filepath.EvalSymlinks(s.Context.RepoRoot)
	require.NoError(t, err)
	canonicalWorktreeSwitchPath, err := filepath.EvalSymlinks(result.WorktreeSwitchPath)
	require.NoError(t, err)
	require.Equal(t, canonicalRepoRoot, canonicalWorktreeSwitchPath)
	require.Equal(t, "feature", result.TargetBranch)
}
