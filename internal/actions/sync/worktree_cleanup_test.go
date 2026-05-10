package sync_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/actions/sync"
	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

func TestSyncCleansOrphanedWorktrees(t *testing.T) {
	t.Run("cleans worktree when stack root branch is deleted", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit()

		// Create a branch that will become a stack root
		s.CreateBranch("feature-branch").Commit("feature change")
		s.TrackBranch("feature-branch", "main")

		// Register a fake worktree for this branch (we won't actually create the worktree dir)
		err := s.Engine.RegisterWorktree("feature-branch", "/tmp/fake-worktree-path")
		require.NoError(t, err)

		// Verify worktree is registered
		wt, err := s.Engine.GetWorktreeForStack("feature-branch")
		require.NoError(t, err)
		require.NotNil(t, wt)

		// Delete the branch (simulating what happens when PR is merged and branch is cleaned)
		s.Checkout("main")
		err = s.Engine.DeleteBranch(s.Context.Context, s.Engine.GetBranch("feature-branch"))
		require.NoError(t, err)

		// Run sync - this should clean up the orphaned worktree registration
		handler := &sync.NullHandler{}
		err = sync.Action(s.Context, sync.Options{}, handler)
		require.NoError(t, err)

		// Verify worktree registration was cleaned up
		wt, err = s.Engine.GetWorktreeForStack("feature-branch")
		require.NoError(t, err)
		assert.Nil(t, wt, "worktree registration should be removed after branch deletion")
	})

	t.Run("preserves worktree when stack root branch still exists", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit()

		// Create and track a branch
		s.CreateBranch("feature-branch").Commit("feature change")
		s.TrackBranch("feature-branch", "main")

		// Register a worktree for this branch
		err := s.Engine.RegisterWorktree("feature-branch", "/tmp/fake-worktree-path")
		require.NoError(t, err)

		// Go back to main and run sync
		s.Checkout("main")
		handler := &sync.NullHandler{}
		err = sync.Action(s.Context, sync.Options{}, handler)
		require.NoError(t, err)

		// Verify worktree registration is preserved
		wt, err := s.Engine.GetWorktreeForStack("feature-branch")
		require.NoError(t, err)
		assert.NotNil(t, wt, "worktree registration should be preserved when branch exists")
	})
}
