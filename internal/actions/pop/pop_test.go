package pop_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/actions/pop"
	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

func TestPopAction(t *testing.T) {
	t.Parallel()
	t.Run("pops branch and retains changes as staged", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
			})

		// Switch to branch1
		s.Checkout("branch1")

		// Pop the branch
		err := pop.Action(t.Context(), deps(s))
		require.NoError(t, err)

		// Verify we're now on main
		currentBranch, err := s.Scene.Repo.CurrentBranchName()
		require.NoError(t, err)
		require.Equal(t, "main", currentBranch)

		// Verify branch1 is deleted
		branches, err := s.Git.GetAllBranchNames(t.Context())
		require.NoError(t, err)
		require.NotContains(t, branches, "branch1")

		// Verify changes are staged
		hasStaged, err := s.Git.HasStagedChanges(s.Context.Context)
		require.NoError(t, err)
		require.True(t, hasStaged, "Changes should be staged after pop")
	})

	t.Run("reparents children when popping branch", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
				"branch2": "branch1",
			})

		// Switch to branch1 and rebuilding is automatic
		s.Checkout("branch1")

		// Pop branch1
		err := pop.Action(t.Context(), deps(s))
		require.NoError(t, err)

		// Verify branch2's parent is now main
		branch := s.Engine.GetBranch("branch2")
		parent := branch.Parent()
		require.NotNil(t, parent)
		require.Equal(t, "main", parent.Name())

		// Verify branch1 is deleted
		branches, err := s.Git.GetAllBranchNames(t.Context())
		require.NoError(t, err)
		require.NotContains(t, branches, "branch1")
	})

	t.Run("fails when trying to pop trunk", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

		// Try to pop trunk (main)
		err := pop.Action(t.Context(), deps(s))
		require.Error(t, err)
		require.Contains(t, err.Error(), "cannot pop trunk branch")
	})

	t.Run("fails when trying to pop untracked branch", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			CreateBranch("untracked")

		// Try to pop untracked branch
		err := pop.Action(t.Context(), deps(s))
		require.Error(t, err)
		require.Contains(t, err.Error(), "is not tracked by stackit")
	})

	t.Run("fails when rebase is in progress", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
			})

		// Manually create a rebase-merge directory to simulate rebase in progress
		rebasePath := filepath.Join(s.Scene.Dir, ".git", "rebase-merge")
		err := os.MkdirAll(rebasePath, 0755)
		require.NoError(t, err)

		// Verify rebase is detected as in progress
		require.True(t, s.Scene.Repo.RebaseInProgress(), "Rebase should be in progress")

		// Try to pop during rebase
		err = pop.Action(t.Context(), deps(s))
		require.Error(t, err)
		require.Contains(t, err.Error(), "rebase is already in progress")
	})

	t.Run("fails when there are uncommitted changes", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
			}).
			WithUncommittedChange("dirty")

		// Try to pop with dirty tree
		err := pop.Action(t.Context(), deps(s))
		require.Error(t, err)
		require.Contains(t, err.Error(), "uncommitted changes")
	})
}

func deps(s *scenario.Scenario) pop.Deps {
	return pop.Deps{Engine: s.Context.Engine, Out: s.Context.Output}
}
