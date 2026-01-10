package undo

import (
	"testing"

	"github.com/stretchr/testify/require"

	"stackit.dev/stackit/internal/engine"
	"stackit.dev/stackit/testhelpers"
	"stackit.dev/stackit/testhelpers/scenario"
)

func TestUndoAction(t *testing.T) {
	t.Run("returns error when no snapshots exist", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit()

		err := Action(s.Context, Options{}, nil)
		require.NoError(t, err) // Should not error, just show message
	})

	t.Run("restores to snapshot when only one exists", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit().
			CreateBranch("feature").
			Commit("feature change").
			Checkout("main").
			TrackBranch("feature", "main")

		// Get initial state
		initialFeatureSHA, err := s.Engine.GetBranch("feature").GetRevision()
		require.NoError(t, err)

		// Take snapshot
		err = s.Engine.TakeSnapshot(engine.SnapshotOptions{
			Command: "move",
			Args:    []string{"feature", "onto", "main"},
		})
		require.NoError(t, err)

		// Make changes
		s.Checkout("feature").
			Commit("additional change")

		// Verify SHA changed
		newFeatureSHA, err := s.Engine.GetBranch("feature").GetRevision()
		require.NoError(t, err)
		require.NotEqual(t, initialFeatureSHA, newFeatureSHA)

		// Test restore directly via engine (bypasses confirmation prompt)
		snapshots, err := s.Engine.GetSnapshots()
		require.NoError(t, err)
		require.Len(t, snapshots, 1)

		err = s.Engine.RestoreSnapshot(s.Context, snapshots[0].ID)
		require.NoError(t, err)

		// Verify state restored
		s.Engine.Rebuild(s.Engine.Trunk().GetName())
		restoredFeatureSHA, err := s.Engine.GetBranch("feature").GetRevision()
		require.NoError(t, err)
		require.Equal(t, initialFeatureSHA, restoredFeatureSHA)
	})

	t.Run("returns error for invalid snapshot ID", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit()

		// Create at least one snapshot so GetSnapshots doesn't return empty
		err := s.Engine.TakeSnapshot(engine.SnapshotOptions{Command: "test"})
		require.NoError(t, err)

		err = Action(s.Context, Options{
			SnapshotID: "nonexistent-snapshot",
		}, nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "not found")
	})

	t.Run("restores after multiple operations", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit().
			CreateBranch("feature1").
			Commit("feature1 change").
			Checkout("main").
			TrackBranch("feature1", "main")

		// Get initial state BEFORE taking snapshot
		initialFeature1SHA, err := s.Engine.GetBranch("feature1").GetRevision()
		require.NoError(t, err)

		// Take first snapshot (captures initial state)
		err = s.Engine.TakeSnapshot(engine.SnapshotOptions{
			Command: "create",
			Args:    []string{"feature1"},
		})
		require.NoError(t, err)

		// Make first change
		s.Checkout("feature1").
			Commit("feature1 change 2")

		// Get SHA after first change
		afterFirstChangeSHA, err := s.Engine.GetBranch("feature1").GetRevision()
		require.NoError(t, err)
		require.NotEqual(t, initialFeature1SHA, afterFirstChangeSHA)

		// Take second snapshot (captures state after first change)
		err = s.Engine.TakeSnapshot(engine.SnapshotOptions{
			Command: "move",
			Args:    []string{"feature1", "onto", "main"},
		})
		require.NoError(t, err)

		// Make second change
		s.Commit("feature1 change 3")

		// Get SHA after second change
		afterSecondChangeSHA, err := s.Engine.GetBranch("feature1").GetRevision()
		require.NoError(t, err)
		require.NotEqual(t, afterFirstChangeSHA, afterSecondChangeSHA)

		// Restore to first snapshot (undoing second operation) via engine directly
		snapshots, err := s.Engine.GetSnapshots()
		require.NoError(t, err)
		require.Len(t, snapshots, 2)

		// Restore to the most recent snapshot (Snapshot 2, taken after first change)
		// Since GetSnapshots returns newest first, this is index 0
		err = s.Engine.RestoreSnapshot(s.Context, snapshots[0].ID)
		require.NoError(t, err)

		// Verify state restored to Snapshot 2 (after first change, not initial)
		s.Engine.Rebuild(s.Engine.Trunk().GetName())
		restoredFeature1SHA, err := s.Engine.GetBranch("feature1").GetRevision()
		require.NoError(t, err)
		// Should restore to state after first change
		require.Equal(t, afterFirstChangeSHA, restoredFeature1SHA)
	})
}

func TestUndoAfterCreate(t *testing.T) {
	t.Run("undoes branch creation", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit()

		// Take snapshot BEFORE creating branch
		err := s.Engine.TakeSnapshot(engine.SnapshotOptions{
			Command: "create",
			Args:    []string{"feature"},
		})
		require.NoError(t, err)

		// Create branch after snapshot
		s.CreateBranch("feature").
			Commit("feature change").
			Checkout("main")

		// Track the branch
		err = s.Engine.TrackBranch(s.Context, "feature", "main")
		require.NoError(t, err)

		// Verify branch exists
		allBranches := s.Engine.AllBranches()
		branches := make([]string, len(allBranches))
		for i, b := range allBranches {
			branches[i] = b.GetName()
		}
		require.Contains(t, branches, "feature")

		// Undo via engine directly (bypasses confirmation)
		snapshots, err := s.Engine.GetSnapshots()
		require.NoError(t, err)
		err = s.Engine.RestoreSnapshot(s.Context, snapshots[0].ID)
		require.NoError(t, err)

		// Verify branch was deleted (it didn't exist in the snapshot)
		s.Engine.Rebuild(s.Engine.Trunk().GetName())
		allBranches2 := s.Engine.AllBranches()
		branches = make([]string, len(allBranches2))
		for i, b := range allBranches2 {
			branches[i] = b.GetName()
		}
		require.NotContains(t, branches, "feature")
	})
}

func TestUndoAfterMove(t *testing.T) {
	t.Run("undoes branch move operation", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit().
			CreateBranch("feature1").
			Commit("feature1 change").
			CreateBranch("feature2").
			Commit("feature2 change").
			Checkout("main").
			TrackBranch("feature1", "main").
			TrackBranch("feature2", "feature1")

		// Get initial parent
		branchinitialParent := s.Engine.GetBranch("feature2")
		initialParent := branchinitialParent.GetParent()
		require.NotNil(t, initialParent)
		require.Equal(t, "feature1", initialParent.GetName())

		// Take snapshot before move
		err := s.Engine.TakeSnapshot(engine.SnapshotOptions{
			Command: "move",
			Args:    []string{"feature2", "onto", "main"},
		})
		require.NoError(t, err)

		// Move feature2 to main
		err = s.Engine.SetParent(s.Context, s.Engine.GetBranch("feature2"), s.Engine.GetBranch("main"))
		require.NoError(t, err)

		// Verify parent changed
		branchnewParent := s.Engine.GetBranch("feature2")
		newParent := branchnewParent.GetParent()
		require.NotNil(t, newParent)
		require.Equal(t, "main", newParent.GetName())

		// Undo via engine directly (bypasses confirmation)
		snapshots, err := s.Engine.GetSnapshots()
		require.NoError(t, err)
		err = s.Engine.RestoreSnapshot(s.Context, snapshots[0].ID)
		require.NoError(t, err)

		// Verify parent restored
		s.Engine.Rebuild(s.Engine.Trunk().GetName())
		branchrestoredParent := s.Engine.GetBranch("feature2")
		restoredParent := branchrestoredParent.GetParent()
		require.NotNil(t, restoredParent)
		require.Equal(t, initialParent.GetName(), restoredParent.GetName())
	})
}
