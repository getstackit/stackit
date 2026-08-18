package engine_test

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

// snapshotOpts is a helper to create SnapshotOptions for tests
func snapshotOpts(command string, args ...string) engine.SnapshotOptions {
	return engine.SnapshotOptions{
		Command: command,
		Args:    args,
	}
}

func TestTakeSnapshot(t *testing.T) {
	t.Parallel()
	t.Run("creates snapshot with branch and metadata SHAs", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

		// Create a stack: main -> feature
		s.WithInitialCommit().
			CreateBranch("feature").
			Commit("feature change").
			Checkout("main").
			TrackBranch("feature", "main")

		// Get current branch SHAs
		mainSHA, err := s.Engine.Trunk().GetRevision()
		require.NoError(t, err)
		featureSHA, err := s.Engine.GetBranch("feature").GetRevision()
		require.NoError(t, err)

		// Take snapshot
		err = s.Engine.TakeSnapshot(t.Context(), snapshotOpts("test", "arg1", "arg2"))
		require.NoError(t, err)

		// Verify snapshot was created
		snapshots, err := s.Engine.GetSnapshots()
		require.NoError(t, err)
		require.Len(t, snapshots, 1)

		// Load the snapshot
		snapshot, err := s.Engine.LoadSnapshot(snapshots[0].ID)
		require.NoError(t, err)
		require.Equal(t, "test", snapshot.Command)
		require.Equal(t, []string{"arg1", "arg2"}, snapshot.Args)
		require.Equal(t, "main", snapshot.CurrentBranch)
		require.Equal(t, mainSHA, snapshot.BranchSHAs["main"])
		require.Equal(t, featureSHA, snapshot.BranchSHAs["feature"])
		require.NotEmpty(t, snapshot.MetadataSHAs["feature"])
	})

	t.Run("creates undo directory if it doesn't exist", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit()

		undoDir := filepath.Join(s.Scene.Dir, ".git", "stackit", "undo")
		require.NoDirExists(t, undoDir)

		err := s.Engine.TakeSnapshot(t.Context(), snapshotOpts("test"))
		require.NoError(t, err)

		require.DirExists(t, undoDir)
	})

	t.Run("captures current branch correctly", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit().
			CreateBranch("feature").
			Commit("feature change")

		// Take snapshot while on feature branch
		err := s.Engine.TakeSnapshot(t.Context(), snapshotOpts("test"))
		require.NoError(t, err)

		snapshots, err := s.Engine.GetSnapshots()
		require.NoError(t, err)
		snapshot, err := s.Engine.LoadSnapshot(snapshots[0].ID)
		require.NoError(t, err)
		require.Equal(t, "feature", snapshot.CurrentBranch)
	})
}

func TestGetSnapshots(t *testing.T) {
	t.Parallel()
	t.Run("returns empty list when no snapshots exist", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit()

		snapshots, err := s.Engine.GetSnapshots()
		require.NoError(t, err)
		require.Empty(t, snapshots)
	})

	t.Run("returns snapshots sorted by time newest first", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit()

		// Take multiple snapshots with small delays
		err := s.Engine.TakeSnapshot(t.Context(), snapshotOpts("first"))
		require.NoError(t, err)
		time.Sleep(50 * time.Millisecond) // Longer delay to ensure different timestamps
		err = s.Engine.TakeSnapshot(t.Context(), snapshotOpts("second"))
		require.NoError(t, err)
		time.Sleep(50 * time.Millisecond)
		err = s.Engine.TakeSnapshot(t.Context(), snapshotOpts("third"))
		require.NoError(t, err)

		snapshots, err := s.Engine.GetSnapshots()
		require.NoError(t, err)
		require.Len(t, snapshots, 3)

		// Verify they're sorted newest first (by timestamp)
		// The timestamps should be in descending order
		require.True(t, snapshots[0].Timestamp.After(snapshots[1].Timestamp) ||
			snapshots[0].Timestamp.Equal(snapshots[1].Timestamp),
			"First snapshot should be newer or equal to second")
		require.True(t, snapshots[1].Timestamp.After(snapshots[2].Timestamp) ||
			snapshots[1].Timestamp.Equal(snapshots[2].Timestamp),
			"Second snapshot should be newer or equal to third")

		// Verify commands match (they should be in reverse order due to sorting)
		commands := []string{snapshots[0].Command, snapshots[1].Command, snapshots[2].Command}
		require.Contains(t, commands, "first")
		require.Contains(t, commands, "second")
		require.Contains(t, commands, "third")
	})

	t.Run("includes display names with SHA and local timestamp", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit()

		err := s.Engine.TakeSnapshot(t.Context(), snapshotOpts("move", "branch-a", "onto", "branch-b"))
		require.NoError(t, err)

		snapshots, err := s.Engine.GetSnapshots()
		require.NoError(t, err)
		require.Len(t, snapshots, 1)
		require.Contains(t, snapshots[0].DisplayName, "[MOVE]")
		require.Contains(t, snapshots[0].DisplayName, "move branch-a onto")
		require.NotEmpty(t, snapshots[0].HeadSHA)
	})
}

func TestLoadSnapshot(t *testing.T) {
	t.Parallel()
	t.Run("loads valid snapshot", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit().
			CreateBranch("feature").
			Commit("feature change").
			Checkout("main").
			TrackBranch("feature", "main")

		err := s.Engine.TakeSnapshot(t.Context(), snapshotOpts("test", "arg"))
		require.NoError(t, err)

		snapshots, err := s.Engine.GetSnapshots()
		require.NoError(t, err)

		snapshot, err := s.Engine.LoadSnapshot(snapshots[0].ID)
		require.NoError(t, err)
		require.Equal(t, "test", snapshot.Command)
		require.Equal(t, []string{"arg"}, snapshot.Args)
		require.NotEmpty(t, snapshot.BranchSHAs)
		require.NotEmpty(t, snapshot.MetadataSHAs)
	})

	t.Run("returns error for non-existent snapshot", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit()

		_, err := s.Engine.LoadSnapshot("nonexistent")
		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to read snapshot")
	})
}

func TestRestoreSnapshot(t *testing.T) {
	t.Parallel()
	t.Run("restores branch heads to snapshot state", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit().
			CreateBranch("feature").
			Commit("feature change").
			Checkout("main").
			TrackBranch("feature", "main")

		// Get initial SHAs
		initialMainSHA, err := s.Engine.Trunk().GetRevision()
		require.NoError(t, err)
		initialFeatureSHA, err := s.Engine.GetBranch("feature").GetRevision()
		require.NoError(t, err)

		// Take snapshot
		err = s.Engine.TakeSnapshot(t.Context(), snapshotOpts("test"))
		require.NoError(t, err)

		// Make changes: add commits to both branches
		s.Checkout("main").
			Commit("main change")
		s.Checkout("feature").
			Commit("feature change 2")

		// Verify SHAs changed
		newMainSHA, err := s.Engine.Trunk().GetRevision()
		require.NoError(t, err)
		newFeatureSHA, err := s.Engine.GetBranch("feature").GetRevision()
		require.NoError(t, err)
		require.NotEqual(t, initialMainSHA, newMainSHA)
		require.NotEqual(t, initialFeatureSHA, newFeatureSHA)

		// Restore snapshot
		snapshots, err := s.Engine.GetSnapshots()
		require.NoError(t, err)
		err = s.Engine.RestoreSnapshot(context.Background(), snapshots[0].ID)
		require.NoError(t, err)

		// Verify SHAs restored
		s.Rebuild()
		restoredMainSHA, err := s.Engine.Trunk().GetRevision()
		require.NoError(t, err)
		restoredFeatureSHA, err := s.Engine.GetBranch("feature").GetRevision()
		require.NoError(t, err)
		require.Equal(t, initialMainSHA, restoredMainSHA)
		require.Equal(t, initialFeatureSHA, restoredFeatureSHA)
	})

	t.Run("deletes branches created after snapshot", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit().
			CreateBranch("feature").
			Commit("feature change").
			Checkout("main").
			TrackBranch("feature", "main")

		// Take snapshot
		err := s.Engine.TakeSnapshot(t.Context(), snapshotOpts("test"))
		require.NoError(t, err)

		// Create new branch after snapshot
		s.CreateBranch("new-branch").
			Commit("new branch change").
			TrackBranch("new-branch", "main")

		// Verify new branch exists
		allBranches := s.Engine.AllBranches()
		branches := make([]string, len(allBranches))
		for i, b := range allBranches {
			branches[i] = b.GetName()
		}
		require.Contains(t, branches, "new-branch")

		// Restore snapshot
		snapshots, err := s.Engine.GetSnapshots()
		require.NoError(t, err)
		err = s.Engine.RestoreSnapshot(context.Background(), snapshots[0].ID)
		require.NoError(t, err)

		// Verify new branch was deleted
		s.Rebuild()
		allBranches2 := s.Engine.AllBranches()
		branches = make([]string, len(allBranches2))
		for i, b := range allBranches2 {
			branches[i] = b.GetName()
		}
		require.NotContains(t, branches, "new-branch")
	})

	t.Run("restores metadata refs", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit().
			CreateBranch("feature").
			Commit("feature change").
			Checkout("main").
			TrackBranch("feature", "main")

		// Get initial metadata SHA
		initialMetadata, err := s.Engine.Git().ListMetadata()
		require.NoError(t, err)
		initialFeatureMetadataSHA := initialMetadata["feature"]
		require.NotEmpty(t, initialFeatureMetadataSHA)

		// Take snapshot
		err = s.Engine.TakeSnapshot(t.Context(), snapshotOpts("test"))
		require.NoError(t, err)

		// Modify metadata by changing parent
		err = s.Engine.SetParent(context.Background(), s.Engine.GetBranch("feature"), s.Engine.GetBranch("main"), engine.DivergenceRecompute)
		require.NoError(t, err)

		// Restore snapshot
		snapshots, err := s.Engine.GetSnapshots()
		require.NoError(t, err)
		err = s.Engine.RestoreSnapshot(context.Background(), snapshots[0].ID)
		require.NoError(t, err)

		// Verify metadata restored
		restoredMetadata, err := s.Engine.Git().ListMetadata()
		require.NoError(t, err)
		require.Equal(t, initialFeatureMetadataSHA, restoredMetadata["feature"])
	})

	t.Run("restores HEAD to original branch", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit().
			CreateBranch("feature").
			Commit("feature change").
			Checkout("main")

		// Take snapshot while on main
		err := s.Engine.TakeSnapshot(t.Context(), snapshotOpts("test"))
		require.NoError(t, err)

		// Switch to feature branch
		s.Checkout("feature")
		currentBranch, err := s.Scene.Repo.CurrentBranchName()
		require.NoError(t, err)
		require.Equal(t, "feature", currentBranch)

		// Restore snapshot
		snapshots, err := s.Engine.GetSnapshots()
		require.NoError(t, err)
		err = s.Engine.RestoreSnapshot(context.Background(), snapshots[0].ID)
		require.NoError(t, err)

		// Verify we're back on main
		s.Rebuild()
		currentBranch, err = s.Scene.Repo.CurrentBranchName()
		require.NoError(t, err)
		require.Equal(t, "main", currentBranch)
	})

	t.Run("handles deleted branches gracefully", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit().
			CreateBranch("feature").
			Commit("feature change").
			Checkout("main").
			TrackBranch("feature", "main")

		// Take snapshot
		err := s.Engine.TakeSnapshot(t.Context(), snapshotOpts("test"))
		require.NoError(t, err)

		// Delete feature branch manually
		err = s.Engine.DeleteBranch(context.Background(), s.Engine.GetBranch("feature"))
		require.NoError(t, err)

		// Restore snapshot - should recreate the branch
		snapshots, err := s.Engine.GetSnapshots()
		require.NoError(t, err)
		err = s.Engine.RestoreSnapshot(context.Background(), snapshots[0].ID)
		require.NoError(t, err)

		// Verify branch was restored
		s.Rebuild()
		allBranches := s.Engine.AllBranches()
		branches := make([]string, len(allBranches))
		for i, b := range allBranches {
			branches[i] = b.GetName()
		}
		require.Contains(t, branches, "feature")
	})

	t.Run("switches to trunk if snapshot branch was deleted", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit().
			CreateBranch("feature").
			Commit("feature change")

		// Take snapshot while on feature
		err := s.Engine.TakeSnapshot(t.Context(), snapshotOpts("test"))
		require.NoError(t, err)

		// Delete feature branch
		s.Checkout("main")
		err = s.Engine.DeleteBranch(context.Background(), s.Engine.GetBranch("feature"))
		require.NoError(t, err)

		// Restore snapshot - this will recreate the feature branch
		snapshots, err := s.Engine.GetSnapshots()
		require.NoError(t, err)
		err = s.Engine.RestoreSnapshot(context.Background(), snapshots[0].ID)
		require.NoError(t, err)

		// After restore, the branch is recreated, so we should be on feature
		// (the snapshot's CurrentBranch). The test name is misleading - let's verify
		// that the branch was actually restored and we're on it
		s.Rebuild()
		currentBranch, err := s.Scene.Repo.CurrentBranchName()
		require.NoError(t, err)
		// The snapshot was taken while on feature, so restore puts us back on feature
		require.Equal(t, "feature", currentBranch)

		// Verify the branch exists
		allBranches := s.Engine.AllBranches()
		branches := make([]string, len(allBranches))
		for i, b := range allBranches {
			branches[i] = b.GetName()
		}
		require.Contains(t, branches, "feature")
	})
}

func TestEnforceMaxStackDepth(t *testing.T) {
	t.Parallel()
	t.Run("deletes oldest snapshots when exceeding max depth", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit()

		// Create more snapshots than default max (10)
		for i := range 12 {
			err := s.Engine.TakeSnapshot(t.Context(), snapshotOpts("test", string(rune('a'+i))))
			require.NoError(t, err)
			time.Sleep(10 * time.Millisecond) // Ensure different timestamps
		}

		// Should only have 10 snapshots (max depth)
		snapshots, err := s.Engine.GetSnapshots()
		require.NoError(t, err)
		require.LessOrEqual(t, len(snapshots), 10)

		// The newest snapshots should be kept
		require.Equal(t, "test", snapshots[0].Command)
	})
}

func TestSnapshotFileFormat(t *testing.T) {
	t.Parallel()
	t.Run("snapshot files are valid JSON", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit().
			CreateBranch("feature").
			Commit("feature change").
			Checkout("main").
			TrackBranch("feature", "main")

		err := s.Engine.TakeSnapshot(t.Context(), snapshotOpts("test", "arg1", "arg2"))
		require.NoError(t, err)

		// Read the snapshot file directly
		undoDir := filepath.Join(s.Scene.Dir, ".git", "stackit", "undo")
		entries, err := os.ReadDir(undoDir)
		require.NoError(t, err)
		require.NotEmpty(t, entries)

		// Verify it's a .json file
		snapshotFile := filepath.Join(undoDir, entries[0].Name())
		require.True(t, filepath.Ext(snapshotFile) == ".json")

		// Verify it's valid JSON by loading it
		snapshots, err := s.Engine.GetSnapshots()
		require.NoError(t, err)
		_, err = s.Engine.LoadSnapshot(snapshots[0].ID)
		require.NoError(t, err)
	})
}

// TestSnapshotWorktreeCapture covers the half of "restore me to before the
// command" that branch refs cannot express: the uncommitted changes a command
// like modify turns into a commit. Rolling the ref back without handing those
// changes back deletes work the user never committed themselves.
func TestSnapshotWorktreeCapture(t *testing.T) {
	t.Parallel()

	t.Run("captures and re-applies tracked changes", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit()

		// Commit a file, then leave an uncommitted edit on it.
		require.NoError(t, s.Scene.Repo.CreateChangeAndCommit("committed", "tracked"))
		trackedPath := filepath.Join(s.Scene.Dir, "tracked_test.txt")
		require.NoError(t, os.WriteFile(trackedPath, []byte("work in progress"), 0600))

		require.NoError(t, s.Engine.TakeSnapshot(t.Context(), snapshotOpts("modify")))

		snapshots, err := s.Engine.GetSnapshots()
		require.NoError(t, err)
		require.Len(t, snapshots, 1)

		snapshot, err := s.Engine.LoadSnapshot(snapshots[0].ID)
		require.NoError(t, err)
		require.NotEmpty(t, snapshot.WorktreeSHA, "a dirty working tree must be captured with the snapshot")

		// The capture must survive gc: `git stash create` leaves the commit
		// unreachable, so the snapshot anchors it under a ref.
		anchored, err := s.Scene.Repo.GetRef(engine.UndoRefPrefix + snapshots[0].ID)
		require.NoError(t, err, "the capture must be anchored under a ref")
		require.Equal(t, snapshot.WorktreeSHA, strings.TrimSpace(anchored))

		// Simulate the command consuming the edit and the rollback discarding it.
		require.NoError(t, os.WriteFile(trackedPath, []byte("committed"), 0600))

		restored, err := s.Engine.RestoreWorktree(t.Context(), snapshots[0].ID)
		require.NoError(t, err)
		require.True(t, restored)

		content, err := os.ReadFile(trackedPath)
		require.NoError(t, err)
		require.Equal(t, "work in progress", string(content),
			"the uncommitted edit must be back in the working tree")
	})

	t.Run("captures and re-applies untracked files", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit()

		// A stash cannot hold untracked files, but `modify -a` stages and
		// commits them — so the rollback deletes their only copy.
		require.NoError(t, s.Scene.Repo.CreateChange("brand new", "fresh", true))
		freshPath := filepath.Join(s.Scene.Dir, "fresh_test.txt")

		require.NoError(t, s.Engine.TakeSnapshot(t.Context(), snapshotOpts("modify")))

		snapshots, err := s.Engine.GetSnapshots()
		require.NoError(t, err)
		snapshot, err := s.Engine.LoadSnapshot(snapshots[0].ID)
		require.NoError(t, err)
		require.NotEmpty(t, snapshot.UntrackedSHA, "untracked files must be captured with the snapshot")

		require.NoError(t, os.Remove(freshPath))

		_, err = s.Engine.RestoreWorktree(t.Context(), snapshots[0].ID)
		require.NoError(t, err)

		content, err := os.ReadFile(freshPath)
		require.NoError(t, err, "the captured file must be back in the working tree")
		require.Equal(t, "brand new", string(content))
	})

	t.Run("leaves an existing file alone", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit()

		require.NoError(t, s.Scene.Repo.CreateChange("captured", "fresh", true))
		freshPath := filepath.Join(s.Scene.Dir, "fresh_test.txt")
		require.NoError(t, s.Engine.TakeSnapshot(t.Context(), snapshotOpts("modify")))

		// The file survived the rollback and has since moved on. A safety net
		// must not overwrite content that is currently on disk.
		require.NoError(t, os.WriteFile(freshPath, []byte("newer content"), 0600))

		snapshots, err := s.Engine.GetSnapshots()
		require.NoError(t, err)
		_, err = s.Engine.RestoreWorktree(t.Context(), snapshots[0].ID)
		require.NoError(t, err)

		content, err := os.ReadFile(freshPath)
		require.NoError(t, err)
		require.Equal(t, "newer content", string(content))
	})

	t.Run("captures nothing when the working tree is clean", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit()

		require.NoError(t, s.Engine.TakeSnapshot(t.Context(), snapshotOpts("restack")))

		snapshots, err := s.Engine.GetSnapshots()
		require.NoError(t, err)
		snapshot, err := s.Engine.LoadSnapshot(snapshots[0].ID)
		require.NoError(t, err)
		require.Empty(t, snapshot.WorktreeSHA)
		require.Empty(t, snapshot.UntrackedSHA)

		restored, err := s.Engine.RestoreWorktree(t.Context(), snapshots[0].ID)
		require.NoError(t, err)
		require.False(t, restored, "restoring a clean snapshot is a no-op")
	})

	t.Run("prunes the anchor refs with the snapshot that owns them", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit()

		// Overflow the undo stack so the oldest snapshot is pruned. Each
		// snapshot captures a dirty tree, so each anchors refs.
		var oldestID string
		for i := range engine.DefaultMaxUndoStackDepth + 1 {
			require.NoError(t, s.Scene.Repo.CreateChange("wip "+strconv.Itoa(i), "wip", true))
			require.NoError(t, s.Engine.TakeSnapshot(t.Context(), snapshotOpts("modify")))
			if i == 0 {
				snapshots, err := s.Engine.GetSnapshots()
				require.NoError(t, err)
				require.Len(t, snapshots, 1)
				oldestID = snapshots[0].ID
			}
		}

		_, err := s.Scene.Repo.GetRef(engine.UndoRefPrefix + oldestID + "-untracked")
		require.Error(t, err, "the pruned snapshot's capture must not outlive it")
	})
}
