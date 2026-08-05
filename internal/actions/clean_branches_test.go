package actions_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/actions"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

type countingDeletionEngine struct {
	engine.Engine
	listWorktreesCalls        int
	deleteBranchesCalls       int
	deleteRemoteMetadataCalls int
}

func (e *countingDeletionEngine) ListWorktrees(ctx context.Context) (git.WorktreeList, error) {
	e.listWorktreesCalls++
	return e.Engine.ListWorktrees(ctx)
}

func (e *countingDeletionEngine) DeleteBranches(ctx context.Context, branches engine.Branches) ([]string, error) {
	e.deleteBranchesCalls++
	return e.Engine.DeleteBranches(ctx, branches)
}

func (e *countingDeletionEngine) DeleteRemoteMetadataForBranches(ctx context.Context, branchNames []string) error {
	e.deleteRemoteMetadataCalls++
	return e.Engine.DeleteRemoteMetadataForBranches(ctx, branchNames)
}

// fixedWorktreeEngine reports a caller-supplied worktree list, so a test can
// describe a checkout in a worktree other than the one it runs in.
type fixedWorktreeEngine struct {
	engine.Engine
	list git.WorktreeList
}

func (e *fixedWorktreeEngine) ListWorktrees(context.Context) (git.WorktreeList, error) {
	return e.list, nil
}

func TestCleanBranches(t *testing.T) {
	t.Parallel()
	t.Run("deletes merged branch and updates children", func(t *testing.T) {
		t.Parallel()
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
		t.Parallel()
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

	// A merged branch checked out in the *main* working tree cannot be deleted
	// from anywhere else: that worktree cannot be removed to free the branch,
	// and only the engine's own HEAD gets switched to trunk automatically.
	//
	// It used to be reported as ready to delete anyway. Refs are deleted in one
	// atomic batch, so git rejected the entire batch over that single branch and
	// every other merged branch stayed behind too, under one confusing error.
	t.Run("skips branch checked out in another worktree without aborting the batch", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
				"branch2": "main",
			})

		s.Checkout("main").
			RunGit("merge", "branch1").
			RunGit("merge", "branch2")
		require.NoError(t, s.Engine.Rebuild("main"))

		for i, name := range []string{"branch1", "branch2"} {
			prInfo := testhelpers.NewTestPrInfoMerged(i+1, "main")
			require.NoError(t, s.Engine.UpsertPrInfo(context.Background(), s.Engine.GetBranch(name), prInfo))
		}

		// Stand in for running from a linked worktree: the main working tree is
		// somewhere else and has branch1 checked out.
		s.Context.Engine = &fixedWorktreeEngine{
			Engine: s.Engine,
			list: git.WorktreeList{
				{Path: t.TempDir(), Branch: "branch1"},
				{Path: s.Scene.Repo.Dir, Branch: "main"},
			},
		}

		result, err := actions.CleanBranches(s.Context, actions.CleanBranchesOptions{
			Force: true,
		})
		require.NoError(t, err)

		require.Contains(t, result.SkippedCheckedOut, "branch1")
		require.True(t, s.Engine.GetBranch("branch1").IsTracked())
		require.NotContains(t, result.DeletedBranches, "branch1")

		// The rest of the batch still gets cleaned.
		require.False(t, s.Engine.GetBranch("branch2").IsTracked())
		require.Contains(t, result.DeletedBranches, "branch2")
	})

	t.Run("does not delete branch without PR when not merged", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
			})
		countingEngine := &countingDeletionEngine{Engine: s.Engine}
		s.Context.Engine = countingEngine

		result, err := actions.CleanBranches(s.Context, actions.CleanBranchesOptions{
			Force: false,
		})
		require.NoError(t, err)

		// Branch should still exist
		require.True(t, s.Engine.GetBranch("branch1").IsTracked())
		require.Empty(t, result.BranchesWithNewParents)
		require.Zero(t, countingEngine.listWorktreesCalls)
		require.Zero(t, countingEngine.deleteBranchesCalls)
		require.Zero(t, countingEngine.deleteRemoteMetadataCalls)
	})

	t.Run("deletes locked branch when merged", func(t *testing.T) {
		t.Parallel()
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
		t.Parallel()
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
		t.Parallel()
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

	t.Run("deletes a fully merged stack in one cleanup pass", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
				"branch2": "branch1",
				"branch3": "branch2",
			})

		for number, name := range []string{"branch1", "branch2", "branch3"} {
			err := s.Engine.UpsertPrInfo(context.Background(), s.Engine.GetBranch(name), testhelpers.NewTestPrInfoMerged(number+1, "main"))
			require.NoError(t, err)
		}
		countingEngine := &countingDeletionEngine{Engine: s.Engine}
		s.Context.Engine = countingEngine

		result, err := actions.CleanBranches(s.Context, actions.CleanBranchesOptions{Force: true})
		require.NoError(t, err)
		require.Len(t, result.DeletedBranches, 3)
		require.Equal(t, 1, countingEngine.listWorktreesCalls)
		require.Equal(t, 1, countingEngine.deleteBranchesCalls)
		require.Equal(t, 1, countingEngine.deleteRemoteMetadataCalls)
		for _, name := range []string{"branch1", "branch2", "branch3"} {
			require.Contains(t, result.DeletedBranches, name)
			require.False(t, s.Engine.GetBranch(name).IsTracked())
		}
	})

	t.Run("marks branch with unpushed changes when merged", func(t *testing.T) {
		t.Parallel()
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

		s.Checkout("main")

		plan, err := actions.PlanBranchDeletions(s.Context, actions.CleanBranchesOptions{
			Force: true,
		})
		require.NoError(t, err)

		// branch1 should be in BranchesToDelete but also in UnpushedBranches
		require.Contains(t, plan.BranchesToDelete, "branch1")
		require.True(t, plan.UnpushedBranches["branch1"], "branch1 should be marked as having unpushed changes")
	})

	t.Run("marks a closed PR with a deleted remote branch as unpushed", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
			})

		_, err := s.Scene.Repo.CreateBareRemote("origin")
		require.NoError(t, err)
		require.NoError(t, s.Scene.Repo.PushBranch("origin", "main"))
		require.NoError(t, s.Scene.Repo.PushBranch("origin", "branch1"))

		s.Checkout("branch1").CommitChange("follow-up.txt", "local-only follow-up").Checkout("main")
		require.NoError(t, s.Scene.Repo.RunGitCommand("push", "origin", "--delete", "branch1"))
		require.NoError(t, s.Engine.UpsertPrInfo(context.Background(), s.Engine.GetBranch("branch1"), testhelpers.NewTestPrInfoClosed(1)))

		plan, err := actions.PlanBranchDeletions(s.Context, actions.CleanBranchesOptions{Force: true})
		require.NoError(t, err)
		require.Contains(t, plan.BranchesToDelete, "branch1")
		require.True(t, plan.UnpushedBranches["branch1"], "missing remote must preserve unverifiable local work")
	})

	t.Run("does not mark a merged PR with a deleted remote branch as unpushed", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{"branch1": "main"})

		_, err := s.Scene.Repo.CreateBareRemote("origin")
		require.NoError(t, err)
		require.NoError(t, s.Scene.Repo.PushBranch("origin", "main"))
		require.NoError(t, s.Scene.Repo.PushBranch("origin", "branch1"))
		require.NoError(t, s.Scene.Repo.RunGitCommand("push", "origin", "--delete", "branch1"))
		require.NoError(t, s.Engine.UpsertPrInfo(context.Background(), s.Engine.GetBranch("branch1"), testhelpers.NewTestPrInfoMerged(1, "main")))

		plan, err := actions.PlanBranchDeletions(s.Context, actions.CleanBranchesOptions{Force: true})
		require.NoError(t, err)
		require.Contains(t, plan.BranchesToDelete, "branch1")
		require.False(t, plan.UnpushedBranches["branch1"])
	})

	t.Run("planning does not reparent surviving children", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
				"branch2": "branch1",
			})

		prInfo := testhelpers.NewTestPrInfoMerged(1, "main")
		err := s.Engine.UpsertPrInfo(context.Background(), s.Engine.GetBranch("branch1"), prInfo)
		require.NoError(t, err)

		plan, err := actions.PlanBranchDeletions(s.Context, actions.CleanBranchesOptions{
			Force: true,
		})
		require.NoError(t, err)
		require.Contains(t, plan.BranchesToDelete, "branch1")
		require.Contains(t, plan.BranchesWithNewParents, "branch2")

		parent := s.Engine.GetBranch("branch2").GetParent()
		require.NotNil(t, parent)
		require.Equal(t, "branch1", parent.GetName(), "planning should not mutate branch metadata")
	})

	t.Run("uses supplied remote statuses for unpushed detection", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
			})

		prInfo := testhelpers.NewTestPrInfoMerged(1, "main")
		err := s.Engine.UpsertPrInfo(context.Background(), s.Engine.GetBranch("branch1"), prInfo)
		require.NoError(t, err)

		plan, err := actions.PlanBranchDeletions(s.Context, actions.CleanBranchesOptions{
			Force: true,
			RemoteStatuses: engine.BranchRemoteStatuses{
				"branch1": {
					LocalSha:       "local",
					RemoteSha:      "remote",
					CommonAncestor: "remote",
				},
			},
		})
		require.NoError(t, err)
		require.Contains(t, plan.BranchesToDelete, "branch1")
		require.True(t, plan.UnpushedBranches["branch1"], "branch1 should use the supplied ahead status")
	})

	t.Run("does not mark branch without unpushed changes as unpushed", func(t *testing.T) {
		t.Parallel()
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

		plan, err := actions.PlanBranchDeletions(s.Context, actions.CleanBranchesOptions{
			Force: true,
		})
		require.NoError(t, err)

		// branch1 should be in BranchesToDelete but NOT in UnpushedBranches
		require.Contains(t, plan.BranchesToDelete, "branch1")
		require.False(t, plan.UnpushedBranches["branch1"], "branch1 should not be marked as having unpushed changes")
	})

	t.Run("preserves divergence when reparenting after squash-merged parent deletion", func(t *testing.T) {
		t.Parallel()
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
