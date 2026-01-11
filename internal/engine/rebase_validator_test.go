package engine_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"stackit.dev/stackit/internal/engine"
	"stackit.dev/stackit/testhelpers"
	"stackit.dev/stackit/testhelpers/scenario"
)

func TestValidateRebases(t *testing.T) {
	t.Parallel()

	t.Run("returns success for empty specs", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

		result, err := s.Engine.ValidateRebases(context.Background(), []engine.RebaseSpec{})
		require.NoError(t, err)
		require.True(t, result.Success)
		require.Empty(t, result.FailedBranch)
		require.Empty(t, result.ErrorMessage)
	})

	t.Run("validates single successful rebase", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
			})

		// Add a commit to main so rebase actually does something
		s.Checkout("main").
			Commit("main update").
			Checkout("branch1")

		// Get revisions
		mainRev, err := s.Engine.GetRevision(s.Engine.Trunk())
		require.NoError(t, err)
		branch1OldBase, err := s.Engine.Git().GetMergeBase("main", "branch1")
		require.NoError(t, err)

		specs := []engine.RebaseSpec{
			{
				Branch:      "branch1",
				NewParent:   mainRev,
				OldUpstream: branch1OldBase,
			},
		}

		result, err := s.Engine.ValidateRebases(context.Background(), specs)
		require.NoError(t, err)
		require.True(t, result.Success)
		require.Empty(t, result.FailedBranch)
		require.NotEmpty(t, result.NewSHAs["branch1"])
	})

	t.Run("detects conflict during rebase", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

		// Create a file on main first
		s.Checkout("main").
			CommitChange("conflicting-file.txt", "original version")

		// Save this as the old base
		oldBase, err := s.Engine.GetRevision(s.Engine.Trunk())
		require.NoError(t, err)

		// Create branch1 from main and modify the file
		s.CreateBranch("branch1").
			CommitChange("conflicting-file.txt", "branch1 version").
			TrackBranch("branch1", "main")

		// Now update main with a DIFFERENT conflicting change
		s.Checkout("main").
			CommitChange("conflicting-file.txt", "main conflicting version")

		mainRev, err := s.Engine.GetRevision(s.Engine.Trunk())
		require.NoError(t, err)

		specs := []engine.RebaseSpec{
			{
				Branch:      "branch1",
				NewParent:   mainRev,
				OldUpstream: oldBase,
			},
		}

		result, err := s.Engine.ValidateRebases(context.Background(), specs)
		require.NoError(t, err)
		require.False(t, result.Success)
		require.Equal(t, "branch1", result.FailedBranch)
		require.Contains(t, result.ErrorMessage, "conflict")
	})

	t.Run("validates chained rebases in sequence", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
				"branch2": "branch1",
				"branch3": "branch2",
			})

		// Add a commit to main
		s.Checkout("main").
			Commit("main update")

		mainRev, err := s.Engine.GetRevision(s.Engine.Trunk())
		require.NoError(t, err)

		branch1OldBase, _ := s.Engine.Git().GetMergeBase("main", "branch1")
		branch1Rev, _ := s.Engine.GetRevision(s.Engine.GetBranch("branch1"))
		branch2Rev, _ := s.Engine.GetRevision(s.Engine.GetBranch("branch2"))

		specs := []engine.RebaseSpec{
			{
				Branch:      "branch1",
				NewParent:   mainRev,
				OldUpstream: branch1OldBase,
			},
			{
				Branch:      "branch2",
				NewParent:   branch1Rev, // Will use rebased SHA from first spec
				OldUpstream: branch1Rev,
			},
			{
				Branch:      "branch3",
				NewParent:   branch2Rev, // Will use rebased SHA from second spec
				OldUpstream: branch2Rev,
			},
		}

		result, err := s.Engine.ValidateRebases(context.Background(), specs)
		require.NoError(t, err)
		require.True(t, result.Success)
		require.NotEmpty(t, result.NewSHAs["branch1"])
		require.NotEmpty(t, result.NewSHAs["branch2"])
		require.NotEmpty(t, result.NewSHAs["branch3"])
	})

	t.Run("stops at first conflict in chain", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

		// Setup: main has a file, branch1 modifies it, branch2 is clean
		s.Checkout("main").
			CommitChange("file.txt", "original")

		mainRev, _ := s.Engine.GetRevision(s.Engine.Trunk())

		// branch1 modifies the same file - will conflict
		s.CreateBranch("branch1").
			CommitChange("file.txt", "branch1 change").
			TrackBranch("branch1", "main")

		branch1Rev, _ := s.Engine.GetRevision(s.Engine.GetBranch("branch1"))

		// branch2 is clean, no conflict
		s.CreateBranch("branch2").
			CommitChange("other-file.txt", "branch2 change").
			TrackBranch("branch2", "branch1")

		// Now update main with conflicting change
		s.Checkout("main").
			CommitChange("file.txt", "main conflicting change")

		newMainRev, _ := s.Engine.GetRevision(s.Engine.Trunk())

		specs := []engine.RebaseSpec{
			{
				Branch:      "branch1",
				NewParent:   newMainRev,
				OldUpstream: mainRev, // Old main before conflict
			},
			{
				Branch:      "branch2",
				NewParent:   branch1Rev,
				OldUpstream: branch1Rev,
			},
		}

		result, err := s.Engine.ValidateRebases(context.Background(), specs)
		require.NoError(t, err)
		require.False(t, result.Success)
		require.Equal(t, "branch1", result.FailedBranch)
		// branch2 should not have been processed
		require.Empty(t, result.NewSHAs["branch2"])
	})

	t.Run("uses rebased SHA for chained descendants when caller passes SHAs", func(t *testing.T) {
		// This test verifies that when specs use SHAs (not branch names) for NewParent,
		// the validator correctly substitutes the rebased SHA for subsequent rebases.
		// This is critical for move operations where descendants depend on parent's new position.
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
				"branch2": "branch1",
			})

		// Add a commit to main that will cause branch1 to have a different SHA after rebase
		s.Checkout("main").
			Commit("main update")

		// Get SHAs (this is what move.go does - resolves to SHAs before validation)
		mainRev, _ := s.Engine.GetRevision(s.Engine.Trunk())
		branch1OldSHA, _ := s.Engine.GetRevision(s.Engine.GetBranch("branch1"))
		branch1OldBase, _ := s.Engine.Git().GetMergeBase("main", "branch1")

		// Build specs using SHAs (not branch names) - mimicking what move.go does
		specs := []engine.RebaseSpec{
			{
				Branch:      "branch1",
				NewParent:   mainRev,
				OldUpstream: branch1OldBase,
			},
			{
				Branch:      "branch2",
				NewParent:   branch1OldSHA, // Using branch1's OLD SHA
				OldUpstream: branch1OldSHA,
			},
		}

		result, err := s.Engine.ValidateRebases(context.Background(), specs)
		require.NoError(t, err)
		require.True(t, result.Success)

		// Both branches should have new SHAs
		require.NotEmpty(t, result.NewSHAs["branch1"])
		require.NotEmpty(t, result.NewSHAs["branch2"])

		// branch1's new SHA should be different from old (it was rebased)
		require.NotEqual(t, branch1OldSHA, result.NewSHAs["branch1"])
	})

	t.Run("does not modify actual branch refs", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
			})

		// Get original SHA
		originalSHA, err := s.Engine.GetRevision(s.Engine.GetBranch("branch1"))
		require.NoError(t, err)

		// Add commit to main
		s.Checkout("main").
			Commit("main update")

		mainRev, _ := s.Engine.GetRevision(s.Engine.Trunk())
		branch1OldBase, _ := s.Engine.Git().GetMergeBase("main", "branch1")

		specs := []engine.RebaseSpec{
			{
				Branch:      "branch1",
				NewParent:   mainRev,
				OldUpstream: branch1OldBase,
			},
		}

		result, err := s.Engine.ValidateRebases(context.Background(), specs)
		require.NoError(t, err)
		require.True(t, result.Success)

		// Verify branch1 SHA is unchanged
		currentSHA, err := s.Engine.GetRevision(s.Engine.GetBranch("branch1"))
		require.NoError(t, err)
		require.Equal(t, originalSHA, currentSHA, "branch ref should not be modified by validation")
	})
}
