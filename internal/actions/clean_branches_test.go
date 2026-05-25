package actions_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/actions"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

func TestCleanBranches(t *testing.T) {
	t.Run("deletes merged branch and updates children", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
				"branch2": "branch1",
			})

		// Merge branch1 into main
		s.Checkout("main").
			RunGit("merge", "branch1")

		// Rebuild to see changes
		err := s.Engine.Rebuild("main")
		require.NoError(t, err)

		// Mark branch1 as merged via PR info
		prInfo := testhelpers.NewTestPrInfoMerged(1, "main")
		branch := s.Engine.GetBranch("branch1")
		err = s.Engine.UpsertPrInfo(context.Background(), branch, prInfo)
		require.NoError(t, err)

		result, err := actions.CleanBranches(s.Context, actions.CleanBranchesOptions{
			Force: true,
		})
		require.NoError(t, err)

		// branch1 should be deleted
		require.False(t, s.Engine.GetBranch("branch1").IsTracked())

		// branch2 should have new parent (main)
		branchparent2 := s.Engine.GetBranch("branch2")
		parent2 := branchparent2.GetParent()
		require.NotNil(t, parent2)
		require.Equal(t, "main", parent2.GetName())
		require.Contains(t, result.BranchesWithNewParents, "branch2")
	})

	t.Run("handles multiple children when parent is deleted", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
				"branch2": "branch1",
				"branch3": "branch1",
			})

		// Merge branch1
		s.Checkout("main").
			RunGit("merge", "branch1")

		// Rebuild to see changes
		err := s.Engine.Rebuild("main")
		require.NoError(t, err)

		// Mark branch1 as merged
		prInfo := testhelpers.NewTestPrInfoWithState(1, "MERGED")
		branch := s.Engine.GetBranch("branch1")
		err = s.Engine.UpsertPrInfo(context.Background(), branch, prInfo)
		require.NoError(t, err)

		result, err := actions.CleanBranches(s.Context, actions.CleanBranchesOptions{
			Force: true,
		})
		require.NoError(t, err)

		// Both children should have new parent
		branchparent2 := s.Engine.GetBranch("branch2")
		parent2 := branchparent2.GetParent()
		require.NotNil(t, parent2)
		require.Equal(t, "main", parent2.GetName())
		branchparent3 := s.Engine.GetBranch("branch3")
		parent3 := branchparent3.GetParent()
		require.NotNil(t, parent3)
		require.Equal(t, "main", parent3.GetName())
		require.Contains(t, result.BranchesWithNewParents, "branch2")
		require.Contains(t, result.BranchesWithNewParents, "branch3")
	})

	t.Run("does not delete branch without PR when not merged", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
			})

		result, err := actions.CleanBranches(s.Context, actions.CleanBranchesOptions{
			Force: false,
		})
		require.NoError(t, err)

		// Branch should still exist
		require.True(t, s.Engine.GetBranch("branch1").IsTracked())
		require.Empty(t, result.BranchesWithNewParents)
	})

	t.Run("deletes locked branch when merged", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
			})

		// Merge branch1 into main
		s.Checkout("main").
			RunGit("merge", "branch1")

		// Rebuild to see changes
		err := s.Engine.Rebuild("main")
		require.NoError(t, err)

		// Lock the branch (simulating consolidation)
		branch := s.Engine.GetBranch("branch1")
		_, err = s.Engine.SetLocked(context.Background(), engine.BranchesOf(branch), engine.LockReasonConsolidating)
		require.NoError(t, err)
		require.True(t, branch.IsLocked(), "branch should be locked")

		// Mark branch1 as merged via PR info
		prInfo := testhelpers.NewTestPrInfoMerged(1, "main")
		err = s.Engine.UpsertPrInfo(context.Background(), branch, prInfo)
		require.NoError(t, err)

		// Clean should delete the locked branch
		_, err = actions.CleanBranches(s.Context, actions.CleanBranchesOptions{
			Force: true,
		})
		require.NoError(t, err)

		// branch1 should be deleted despite being locked
		require.False(t, s.Engine.GetBranch("branch1").IsTracked())
	})

	t.Run("never considers trunk for deletion", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
			})

		// Merge branch1 into main so trunk appears "merged into itself"
		s.Checkout("main").
			RunGit("merge", "branch1")

		err := s.Engine.Rebuild("main")
		require.NoError(t, err)

		// Mark branch1 as merged
		prInfo := testhelpers.NewTestPrInfoMerged(1, "main")
		branch := s.Engine.GetBranch("branch1")
		err = s.Engine.UpsertPrInfo(context.Background(), branch, prInfo)
		require.NoError(t, err)

		result, err := actions.CleanBranches(s.Context, actions.CleanBranchesOptions{
			Force: true,
		})
		require.NoError(t, err)

		// branch1 should be deleted (it's merged)
		require.Contains(t, result.DeletedBranches, "branch1")

		// trunk (main) must NOT be deleted
		require.NotContains(t, result.DeletedBranches, "main")
		require.True(t, s.Engine.GetBranch("main").IsTrunk())
	})

	t.Run("deletes merged child even if parent is NOT merged", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
				"branch2": "branch1",
			})

		// branch1: NOT merged
		// branch2: IS merged
		prInfo := testhelpers.NewTestPrInfoMerged(2, "branch1")
		branch2 := s.Engine.GetBranch("branch2")
		err := s.Engine.UpsertPrInfo(context.Background(), branch2, prInfo)
		require.NoError(t, err)

		_, err = actions.CleanBranches(s.Context, actions.CleanBranchesOptions{
			Force: true,
		})
		require.NoError(t, err)

		// branch2 should be deleted even though we didn't "visit" it via a deleted branch1
		require.False(t, s.Engine.GetBranch("branch2").IsTracked())
		require.True(t, s.Engine.GetBranch("branch1").IsTracked())
	})

	t.Run("marks branch with unpushed changes when merged", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
			})

		// Set up remote and push
		_, err := s.Scene.Repo.CreateBareRemote("origin")
		require.NoError(t, err)
		err = s.Scene.Repo.PushBranch("origin", "main")
		require.NoError(t, err)
		err = s.Scene.Repo.PushBranch("origin", "branch1")
		require.NoError(t, err)

		// Add an unpushed local commit
		s.Checkout("branch1").
			CommitChange("extra.txt", "unpushed work")

		// Mark branch1 as merged via PR info
		prInfo := testhelpers.NewTestPrInfoMerged(1, "main")
		branch := s.Engine.GetBranch("branch1")
		err = s.Engine.UpsertPrInfo(context.Background(), branch, prInfo)
		require.NoError(t, err)

		// Populate remote SHAs so GetBranchRemoteStatus works
		err = s.Engine.PopulateRemoteShas()
		require.NoError(t, err)

		s.Checkout("main")

		plan, err := actions.PlanBranchDeletions(s.Context, actions.CleanBranchesOptions{
			Force: true,
		})
		require.NoError(t, err)

		// branch1 should be in BranchesToDelete but also in UnpushedBranches
		require.Contains(t, plan.BranchesToDelete, "branch1")
		require.True(t, plan.UnpushedBranches["branch1"], "branch1 should be marked as having unpushed changes")
	})

	t.Run("does not mark branch without unpushed changes as unpushed", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
			})

		// Set up remote and push
		_, err := s.Scene.Repo.CreateBareRemote("origin")
		require.NoError(t, err)
		err = s.Scene.Repo.PushBranch("origin", "main")
		require.NoError(t, err)
		err = s.Scene.Repo.PushBranch("origin", "branch1")
		require.NoError(t, err)

		// Mark branch1 as merged via PR info (no extra local commits)
		prInfo := testhelpers.NewTestPrInfoMerged(1, "main")
		branch := s.Engine.GetBranch("branch1")
		err = s.Engine.UpsertPrInfo(context.Background(), branch, prInfo)
		require.NoError(t, err)

		// Populate remote SHAs
		err = s.Engine.PopulateRemoteShas()
		require.NoError(t, err)

		plan, err := actions.PlanBranchDeletions(s.Context, actions.CleanBranchesOptions{
			Force: true,
		})
		require.NoError(t, err)

		// branch1 should be in BranchesToDelete but NOT in UnpushedBranches
		require.Contains(t, plan.BranchesToDelete, "branch1")
		require.False(t, plan.UnpushedBranches["branch1"], "branch1 should not be marked as having unpushed changes")
	})

	t.Run("preserves divergence when reparenting after squash-merged parent deletion", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

		// Create main -> branch1 (2 commits) -> branch2.
		// Multi-commit parent is important: squash merge can make git IsMerged() false
		// even when PR is merged, which used to reset branch2's divergence point.
		s.CreateBranch("branch1")
		s.CommitChange("shared.txt", "branch1-v1")
		s.CommitChange("shared.txt", "branch1-v2")
		s.TrackBranch("branch1", "main")

		s.CreateBranch("branch2")
		s.CommitChange("child.txt", "branch2-change")
		s.TrackBranch("branch2", "branch1")

		branch1Rev, err := s.Engine.GetBranch("branch1").GetRevision()
		require.NoError(t, err)

		// Simulate squash merge of branch1 by adding branch1's final tree state to main in one commit.
		s.Checkout("main")
		s.CommitChange("shared.txt", "branch1-v2")

		// Mark branch1 as merged in PR metadata so cleanup deletes it.
		prInfo := testhelpers.NewTestPrInfoMerged(1, "main")
		err = s.Engine.UpsertPrInfo(context.Background(), s.Engine.GetBranch("branch1"), prInfo)
		require.NoError(t, err)

		_, err = actions.CleanBranches(s.Context, actions.CleanBranchesOptions{
			Force: true,
		})
		require.NoError(t, err)

		// branch1 should be deleted and branch2 reparented to main.
		require.False(t, s.Engine.GetBranch("branch1").IsTracked())
		parent := s.Engine.GetBranch("branch2").GetParent()
		require.NotNil(t, parent)
		require.Equal(t, "main", parent.GetName())

		// Critical regression assertion: preserve old divergence at branch1 tip.
		// If this regresses, restack can replay branch1 commits and cause avoidable conflicts.
		meta2, err := s.Engine.Git().ReadMetadata("branch2")
		require.NoError(t, err)
		require.NotNil(t, meta2.GetParentBranchRevision())
		require.Equal(t, branch1Rev, *meta2.GetParentBranchRevision())
	})
}
