package engine_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

func TestPRBodyUpdateTracking(t *testing.T) {
	t.Run("marks branch as needing PR body update", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit().
			CreateBranch("feature").
			Commit("f1").
			TrackBranch("feature", "main")

		eng := s.Engine

		// Initially no branches need update
		needsUpdate := eng.GetBranchesNeedingPRBodyUpdate()
		require.Empty(t, needsUpdate)

		// Mark branch as needing update
		err := eng.BatchMarkNeedsPRBodyUpdate(context.Background(), []string{"feature"})
		require.NoError(t, err)

		// Now it should be in the list
		needsUpdate = eng.GetBranchesNeedingPRBodyUpdate()
		require.Contains(t, needsUpdate, "feature")
	})

	t.Run("clears PR body update flag", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit().
			CreateBranch("feature").
			Commit("f1").
			TrackBranch("feature", "main")

		eng := s.Engine

		// Mark and then clear
		err := eng.BatchMarkNeedsPRBodyUpdate(context.Background(), []string{"feature"})
		require.NoError(t, err)

		err = eng.ClearNeedsPRBodyUpdate("feature")
		require.NoError(t, err)

		// Should no longer need update
		needsUpdate := eng.GetBranchesNeedingPRBodyUpdate()
		require.NotContains(t, needsUpdate, "feature")
	})

	t.Run("clear is idempotent for non-marked branches", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit().
			CreateBranch("feature").
			Commit("f1").
			TrackBranch("feature", "main")

		eng := s.Engine

		// Clear without marking should not error
		err := eng.ClearNeedsPRBodyUpdate("feature")
		require.NoError(t, err)
	})

	t.Run("persists across engine rebuild", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit().
			CreateBranch("feature").
			Commit("f1").
			TrackBranch("feature", "main")

		eng := s.Engine

		// Mark branch
		err := eng.BatchMarkNeedsPRBodyUpdate(context.Background(), []string{"feature"})
		require.NoError(t, err)

		// Rebuild engine to simulate fresh state
		err = eng.Rebuild("main")
		require.NoError(t, err)

		// Should still need update
		needsUpdate := eng.GetBranchesNeedingPRBodyUpdate()
		require.Contains(t, needsUpdate, "feature")
	})

	t.Run("tracks multiple branches independently", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit().
			CreateBranch("feature-a").
			Commit("a1").
			TrackBranch("feature-a", "main").
			Checkout("main").
			CreateBranch("feature-b").
			Commit("b1").
			TrackBranch("feature-b", "main")

		eng := s.Engine

		// Mark only feature-a
		err := eng.BatchMarkNeedsPRBodyUpdate(context.Background(), []string{"feature-a"})
		require.NoError(t, err)

		needsUpdate := eng.GetBranchesNeedingPRBodyUpdate()
		require.Contains(t, needsUpdate, "feature-a")
		require.NotContains(t, needsUpdate, "feature-b")

		// Mark feature-b too
		err = eng.BatchMarkNeedsPRBodyUpdate(context.Background(), []string{"feature-b"})
		require.NoError(t, err)

		needsUpdate = eng.GetBranchesNeedingPRBodyUpdate()
		require.Contains(t, needsUpdate, "feature-a")
		require.Contains(t, needsUpdate, "feature-b")

		// Clear only feature-a
		err = eng.ClearNeedsPRBodyUpdate("feature-a")
		require.NoError(t, err)

		needsUpdate = eng.GetBranchesNeedingPRBodyUpdate()
		require.NotContains(t, needsUpdate, "feature-a")
		require.Contains(t, needsUpdate, "feature-b")
	})

	t.Run("batch marks multiple branches atomically", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit().
			CreateBranch("feature-a").
			Commit("a1").
			TrackBranch("feature-a", "main").
			Checkout("main").
			CreateBranch("feature-b").
			Commit("b1").
			TrackBranch("feature-b", "main")

		eng := s.Engine

		// Mark both at once
		err := eng.BatchMarkNeedsPRBodyUpdate(context.Background(), []string{"feature-a", "feature-b"})
		require.NoError(t, err)

		needsUpdate := eng.GetBranchesNeedingPRBodyUpdate()
		require.Contains(t, needsUpdate, "feature-a")
		require.Contains(t, needsUpdate, "feature-b")
	})

	t.Run("batch with empty list is no-op", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit()

		eng := s.Engine

		err := eng.BatchMarkNeedsPRBodyUpdate(context.Background(), []string{})
		require.NoError(t, err)

		err = eng.BatchMarkNeedsPRBodyUpdate(context.Background(), nil)
		require.NoError(t, err)
	})
}
