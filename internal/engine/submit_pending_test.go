package engine_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

func TestSubmitPendingFlag(t *testing.T) {
	t.Parallel()

	newScenario := func(t *testing.T) *scenario.Scenario {
		t.Helper()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit().
			CreateBranch("feature").
			Commit("f1").
			TrackBranch("feature", "main")
		return s
	}

	pending := func(t *testing.T, s *scenario.Scenario, branch string) bool {
		t.Helper()
		meta, err := s.Metadata.ReadLocalMetadata(t.Context(), branch).One()
		require.NoError(t, err)
		return meta != nil && meta.SubmitPending
	}

	t.Run("marks and clears", func(t *testing.T) {
		t.Parallel()
		s := newScenario(t)
		require.False(t, pending(t, s, "feature"))

		require.NoError(t, s.Engine.MarkBranchesSubmitPending(t.Context(), []string{"feature"}))
		require.True(t, pending(t, s, "feature"))

		require.NoError(t, s.Engine.ClearBranchesSubmitPending(t.Context(), []string{"feature"}))
		require.False(t, pending(t, s, "feature"))
	})

	t.Run("empty and repeated calls are no-ops", func(t *testing.T) {
		t.Parallel()
		s := newScenario(t)
		require.NoError(t, s.Engine.MarkBranchesSubmitPending(t.Context(), nil))
		require.NoError(t, s.Engine.ClearBranchesSubmitPending(t.Context(), []string{"feature"}))
		require.False(t, pending(t, s, "feature"))

		require.NoError(t, s.Engine.MarkBranchesSubmitPending(t.Context(), []string{"feature"}))
		require.NoError(t, s.Engine.MarkBranchesSubmitPending(t.Context(), []string{"feature"}))
		require.True(t, pending(t, s, "feature"))
	})

	// The two flags share one local-metadata blob, so writing either must
	// carry the other through untouched.
	t.Run("does not disturb the PR body update flag", func(t *testing.T) {
		t.Parallel()
		s := newScenario(t)
		require.NoError(t, s.Engine.MarkBranchesForPRBodyUpdate(t.Context(), []string{"feature"}))

		require.NoError(t, s.Engine.MarkBranchesSubmitPending(t.Context(), []string{"feature"}))
		require.Contains(t, s.Engine.GetBranchesNeedingPRBodyUpdate(), "feature")

		require.NoError(t, s.Engine.ClearBranchesSubmitPending(t.Context(), []string{"feature"}))
		require.Contains(t, s.Engine.GetBranchesNeedingPRBodyUpdate(), "feature")
		require.False(t, pending(t, s, "feature"))
	})
}
