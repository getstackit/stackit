package delete

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"stackit.dev/stackit/testhelpers"
	"stackit.dev/stackit/testhelpers/scenario"
)

func TestDelete(t *testing.T) {
	t.Run("deletes a single branch", func(t *testing.T) {
		s := scenario.NewScenario(t, nil).
			WithStack(map[string]string{
				"branch1": "main",
				"branch2": "branch1",
			})

		_, err := Action(s.Context, Options{
			BranchName: "branch1",
			Force:      true,
		}, nil)
		require.NoError(t, err)

		// branch1 should be gone, branch2 should be reparented to main
		require.False(t, s.Engine.GetBranch("branch1").IsTracked())
		require.True(t, s.Engine.GetBranch("branch2").IsTracked())
		branchparent2 := s.Engine.GetBranch("branch2")
		parent2 := branchparent2.GetParent()
		require.NotNil(t, parent2)
		require.Equal(t, "main", parent2.GetName())
	})

	t.Run("deletes upstack", func(t *testing.T) {
		s := scenario.NewScenario(t, nil).
			WithStack(map[string]string{
				"branch1": "main",
				"branch2": "branch1",
				"branch3": "branch2",
			})

		_, err := Action(s.Context, Options{
			BranchName: "branch1",
			Upstack:    true,
			Force:      true,
		}, nil)
		require.NoError(t, err)

		// All branches should be gone
		require.False(t, s.Engine.GetBranch("branch1").IsTracked())
		require.False(t, s.Engine.GetBranch("branch2").IsTracked())
		require.False(t, s.Engine.GetBranch("branch3").IsTracked())
	})

	t.Run("deletes downstack", func(t *testing.T) {
		s := scenario.NewScenario(t, nil).
			WithStack(map[string]string{
				"branch1": "main",
				"branch2": "branch1",
				"branch3": "branch2",
			})

		_, err := Action(s.Context, Options{
			BranchName: "branch3",
			Downstack:  true,
			Force:      true,
		}, nil)
		require.NoError(t, err)

		// All branches should be gone
		require.False(t, s.Engine.GetBranch("branch1").IsTracked())
		require.False(t, s.Engine.GetBranch("branch2").IsTracked())
		require.False(t, s.Engine.GetBranch("branch3").IsTracked())
	})

	t.Run("fails without force if not merged", func(t *testing.T) {
		s := scenario.NewScenario(t, nil).
			WithStack(map[string]string{
				"branch1": "main",
			})

		// Add a commit to branch1 so it's not merged
		s.Checkout("branch1").Commit("some change")
		s.Engine.Rebuild("main")

		_, err := Action(s.Context, Options{
			BranchName: "branch1",
			Force:      false,
		}, nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "use --force")
	})

	t.Run("deletes current branch and switches to trunk", func(t *testing.T) {
		s := scenario.NewScenario(t, nil).
			WithStack(map[string]string{
				"branch1": "main",
			})

		s.Checkout("branch1")
		currentBranch := s.Engine.CurrentBranch()
		require.NotNil(t, currentBranch)
		require.Equal(t, "branch1", currentBranch.GetName())

		_, err := Action(s.Context, Options{
			BranchName: "branch1",
			Force:      true,
		}, nil)
		require.NoError(t, err)

		currentBranch = s.Engine.CurrentBranch()
		require.NotNil(t, currentBranch)
		require.Equal(t, "main", currentBranch.GetName())
	})

	t.Run("deletes a branch in a branching stack", func(t *testing.T) {
		s := scenario.NewScenario(t, nil).
			WithStack(map[string]string{
				"parent": "main",
				"child1": "parent",
				"child2": "parent",
			})

		_, err := Action(s.Context, Options{
			BranchName: "parent",
			Force:      true,
		}, nil)
		require.NoError(t, err)

		// parent should be gone
		require.False(t, s.Engine.GetBranch("parent").IsTracked())

		// Both children should be reparented to main and still be tracked
		require.True(t, s.Engine.GetBranch("child1").IsTracked())
		require.True(t, s.Engine.GetBranch("child2").IsTracked())
		branchparent1 := s.Engine.GetBranch("child1")
		parent1 := branchparent1.GetParent()
		require.NotNil(t, parent1)
		require.Equal(t, "main", parent1.GetName())
		branchparent2 := s.Engine.GetBranch("child2")
		parent2 := branchparent2.GetParent()
		require.NotNil(t, parent2)
		require.Equal(t, "main", parent2.GetName())
	})
}

func TestDeleteCleansUpWorktrees(t *testing.T) {
	t.Run("cleans worktree when stack root is deleted", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

		// Create a stack root branch
		s.CreateBranch("feature-branch").Commit("feature change")
		s.TrackBranch("feature-branch", "main")

		// Register a fake worktree for this stack root
		err := s.Engine.RegisterWorktree("feature-branch", "/tmp/fake-worktree-path")
		require.NoError(t, err)

		// Verify worktree is registered
		wt, err := s.Engine.GetWorktreeForStack("feature-branch")
		require.NoError(t, err)
		require.NotNil(t, wt)

		// Delete the stack root
		s.Checkout("main")
		_, err = Action(s.Context, Options{
			BranchName: "feature-branch",
			Force:      true,
		}, nil)
		require.NoError(t, err)

		// Verify worktree registration was cleaned up
		wt, err = s.Engine.GetWorktreeForStack("feature-branch")
		require.NoError(t, err)
		assert.Nil(t, wt, "worktree registration should be removed when stack root is deleted")
	})

	t.Run("does not clean worktree when non-root branch is deleted", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

		// Create a stack with two branches
		s.CreateBranch("stack-root").Commit("root change")
		s.TrackBranch("stack-root", "main")
		s.CreateBranch("child-branch").Commit("child change")
		s.TrackBranch("child-branch", "stack-root")

		// Register a worktree for the stack root
		err := s.Engine.RegisterWorktree("stack-root", "/tmp/fake-worktree-path")
		require.NoError(t, err)

		// Verify worktree is registered
		wt, err := s.Engine.GetWorktreeForStack("stack-root")
		require.NoError(t, err)
		require.NotNil(t, wt)

		// Delete the child branch (not the stack root)
		s.Checkout("stack-root")
		_, err = Action(s.Context, Options{
			BranchName: "child-branch",
			Force:      true,
		}, nil)
		require.NoError(t, err)

		// Verify worktree registration is preserved
		wt, err = s.Engine.GetWorktreeForStack("stack-root")
		require.NoError(t, err)
		assert.NotNil(t, wt, "worktree registration should be preserved when non-root branch is deleted")
	})

	t.Run("cleans worktree when upstack deletes entire stack including root", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

		// Create a stack with multiple branches
		s.CreateBranch("stack-root").Commit("root change")
		s.TrackBranch("stack-root", "main")
		s.CreateBranch("child-branch").Commit("child change")
		s.TrackBranch("child-branch", "stack-root")

		// Register a worktree for the stack root
		err := s.Engine.RegisterWorktree("stack-root", "/tmp/fake-worktree-path")
		require.NoError(t, err)

		// Verify worktree is registered
		wt, err := s.Engine.GetWorktreeForStack("stack-root")
		require.NoError(t, err)
		require.NotNil(t, wt)

		// Delete upstack from stack root (deletes all branches in the stack)
		s.Checkout("main")
		_, err = Action(s.Context, Options{
			BranchName: "stack-root",
			Upstack:    true,
			Force:      true,
		}, nil)
		require.NoError(t, err)

		// Verify worktree registration was cleaned up
		wt, err = s.Engine.GetWorktreeForStack("stack-root")
		require.NoError(t, err)
		assert.Nil(t, wt, "worktree registration should be removed when entire stack is deleted with --upstack")
	})
}
