package engine_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

const prStateMerged = "MERGED"

func TestRestackBranch_CapturesMergedHistory(t *testing.T) {
	t.Parallel()
	t.Run("captures old parent when reparenting due to merge", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
				"branch2": "branch1",
			})

		// Simulate branch1 being merged into main externally
		s.Checkout("main").
			RunGit("merge", "branch1", "--no-ff", "-m", "Merge branch1")

		// Delete branch1 (simulating cleanup after merge)
		s.RunGit("branch", "-D", "branch1")

		// Rebuild to recognize branch1 is gone
		err := s.Engine.Rebuild("main")
		require.NoError(t, err)

		// Restack branch2 - should reparent to main and capture branch1 in history
		branch2 := s.Engine.GetBranch("branch2")
		batchResult, err := s.Engine.RestackBranches(context.Background(), engine.BranchesOf(branch2))
		require.NoError(t, err)

		// Verify reparenting happened
		result := batchResult.Results["branch2"]
		require.True(t, result.Reparented)
		require.Equal(t, "branch1", result.OldParent)
		require.Equal(t, "main", result.NewParent)

		// Verify merged history was captured
		mergedHistory := branch2.GetMergedDownstack()
		require.Len(t, mergedHistory, 1)
		require.Equal(t, "branch1", mergedHistory[0].BranchName)
	})

	t.Run("captures PR info when parent had a PR", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
				"branch2": "branch1",
			})

		// Set PR info on branch1
		branch1 := s.Engine.GetBranch("branch1")
		prNum := 99
		prInfo := engine.NewPrInfoFull(&prNum, "Fix bug", "Body", prStateMerged, "main", "https://github.com/test/99", false, "", "")
		err := s.Engine.UpsertPrInfo(context.Background(), branch1, prInfo)
		require.NoError(t, err)

		// Simulate branch1 being merged (mark it in metadata)
		meta, err := s.Engine.Git().ReadMetadata("branch1")
		require.NoError(t, err)
		mergedState := prStateMerged
		metaPrInfo := meta.GetPrInfo()
		metaPrInfo.State = &mergedState
		meta = meta.WithPrInfo(metaPrInfo)
		err = s.Engine.Git().WriteMetadata("branch1", meta)
		require.NoError(t, err)

		// Rebuild to recognize the state change
		err = s.Engine.Rebuild("main")
		require.NoError(t, err)

		// Restack branch2 - should reparent and capture PR info
		branch2 := s.Engine.GetBranch("branch2")
		_, err = s.Engine.RestackBranches(context.Background(), engine.BranchesOf(branch2))
		require.NoError(t, err)

		// Verify merged history includes PR info
		mergedHistory := branch2.GetMergedDownstack()
		require.Len(t, mergedHistory, 1)
		require.Equal(t, "branch1", mergedHistory[0].BranchName)
		require.NotNil(t, mergedHistory[0].PRNumber)
		require.Equal(t, 99, *mergedHistory[0].PRNumber)
		require.NotNil(t, mergedHistory[0].PRState)
		require.Equal(t, "MERGED", *mergedHistory[0].PRState)
	})
}

func TestRestackBranch_InheritsMergedHistory(t *testing.T) {
	t.Parallel()
	t.Run("inherits history from old parent for multi-level reparenting", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
				"branch2": "branch1",
				"branch3": "branch2",
			})

		// First: merge branch1 and reparent branch2 to main
		meta1, _ := s.Engine.Git().ReadMetadata("branch1")
		mergedState := prStateMerged
		meta1 = meta1.WithPrInfo(&git.PrInfoPersistence{State: &mergedState})
		_ = s.Engine.Git().WriteMetadata("branch1", meta1)

		// Rebuild and restack branch2
		_ = s.Engine.Rebuild("main")
		branch2 := s.Engine.GetBranch("branch2")
		_, _ = s.Engine.RestackBranches(context.Background(), engine.BranchesOf(branch2))

		// Verify branch2 now has branch1 in history
		history2 := branch2.GetMergedDownstack()
		require.Len(t, history2, 1)
		require.Equal(t, "branch1", history2[0].BranchName)

		// Second: merge branch2 and reparent branch3 to main
		meta2, _ := s.Engine.Git().ReadMetadata("branch2")
		meta2 = meta2.WithPrInfo(&git.PrInfoPersistence{State: &mergedState})
		_ = s.Engine.Git().WriteMetadata("branch2", meta2)

		// Rebuild and restack branch3
		_ = s.Engine.Rebuild("main")
		branch3 := s.Engine.GetBranch("branch3")
		_, err := s.Engine.RestackBranches(context.Background(), engine.BranchesOf(branch3))
		require.NoError(t, err)

		// Verify branch3 inherited history: [branch1, branch2]
		history3 := branch3.GetMergedDownstack()
		require.Len(t, history3, 2)
		require.Equal(t, "branch1", history3[0].BranchName) // oldest first
		require.Equal(t, "branch2", history3[1].BranchName) // newest last
	})
}

func TestRestackBranch_LimitsHistoryGrowth(t *testing.T) {
	t.Parallel()
	t.Run("limits history to 5 entries", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

		// Create a deep stack of 7 branches
		branchNames := []string{"b1", "b2", "b3", "b4", "b5", "b6", "b7"}
		parentMap := map[string]string{
			"b1": "main",
			"b2": "b1",
			"b3": "b2",
			"b4": "b3",
			"b5": "b4",
			"b6": "b5",
			"b7": "b6",
		}
		s = s.WithStack(parentMap)

		mergedState := prStateMerged

		// Merge each branch one by one, causing reparenting up the chain
		for i := range 6 {
			branchToMerge := branchNames[i]
			childBranch := branchNames[i+1]

			// Mark branch as merged
			meta, _ := s.Engine.Git().ReadMetadata(branchToMerge)
			meta = meta.WithPrInfo(&git.PrInfoPersistence{State: &mergedState})
			_ = s.Engine.Git().WriteMetadata(branchToMerge, meta)

			// Rebuild and restack child
			_ = s.Engine.Rebuild("main")
			child := s.Engine.GetBranch(childBranch)
			_, _ = s.Engine.RestackBranches(context.Background(), engine.BranchesOf(child))
		}

		// Verify b7 has exactly 5 entries (the most recent 5)
		b7 := s.Engine.GetBranch("b7")
		history := b7.GetMergedDownstack()
		require.Len(t, history, 5)

		// Should have b2-b6 (b1 was dropped due to limit)
		require.Equal(t, "b2", history[0].BranchName)
		require.Equal(t, "b3", history[1].BranchName)
		require.Equal(t, "b4", history[2].BranchName)
		require.Equal(t, "b5", history[3].BranchName)
		require.Equal(t, "b6", history[4].BranchName)
	})
}

func TestRestackBranch_HandlesNoPRInfo(t *testing.T) {
	t.Parallel()
	t.Run("captures branch name when no PR info", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
				"branch2": "branch1",
			})

		// Don't set any PR info on branch1

		// Simulate branch1 being merged by deleting it
		s.Checkout("main").
			RunGit("merge", "branch1", "--no-ff", "-m", "Merge branch1")
		s.RunGit("branch", "-D", "branch1")

		// Rebuild and restack
		err := s.Engine.Rebuild("main")
		require.NoError(t, err)

		branch2 := s.Engine.GetBranch("branch2")
		_, err = s.Engine.RestackBranches(context.Background(), engine.BranchesOf(branch2))
		require.NoError(t, err)

		// Verify history was captured with just the branch name
		mergedHistory := branch2.GetMergedDownstack()
		require.Len(t, mergedHistory, 1)
		require.Equal(t, "branch1", mergedHistory[0].BranchName)
		require.Nil(t, mergedHistory[0].PRNumber) // No PR number
		require.Nil(t, mergedHistory[0].PRState)  // No PR state
	})
}

func TestRestackBranch_PreventsDuplicateHistory(t *testing.T) {
	t.Parallel()
	t.Run("does not add duplicate entries on retried restack", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
				"branch2": "branch1",
			})

		// Mark branch1 as merged
		meta1, _ := s.Engine.Git().ReadMetadata("branch1")
		mergedState := prStateMerged
		meta1 = meta1.WithPrInfo(&git.PrInfoPersistence{State: &mergedState})
		_ = s.Engine.Git().WriteMetadata("branch1", meta1)

		// Rebuild and restack branch2 (first time)
		_ = s.Engine.Rebuild("main")
		branch2 := s.Engine.GetBranch("branch2")
		_, err := s.Engine.RestackBranches(context.Background(), engine.BranchesOf(branch2))
		require.NoError(t, err)

		// Verify branch2 has branch1 in history
		history := branch2.GetMergedDownstack()
		require.Len(t, history, 1)
		require.Equal(t, "branch1", history[0].BranchName)

		// Restack branch2 again (simulating interrupted/retried operation)
		branch2 = s.Engine.GetBranch("branch2")
		_, err = s.Engine.RestackBranches(context.Background(), engine.BranchesOf(branch2))
		require.NoError(t, err)

		// Verify history still has exactly 1 entry (no duplicate)
		history = branch2.GetMergedDownstack()
		require.Len(t, history, 1, "should not add duplicate history entry on retry")
		require.Equal(t, "branch1", history[0].BranchName)
	})
}
