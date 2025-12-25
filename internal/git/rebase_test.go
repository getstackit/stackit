package git_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"stackit.dev/stackit/internal/git"
	"stackit.dev/stackit/testhelpers"
)

func TestRebase(t *testing.T) {
	t.Run("rebases branch onto parent", func(t *testing.T) {
		scene := testhelpers.NewScene(t, func(s *testhelpers.Scene) error {
			return s.Repo.CreateChangeAndCommit("initial", "init")
		})

		// Create branch1
		err := scene.Repo.CreateAndCheckoutBranch("branch1")
		require.NoError(t, err)
		err = scene.Repo.CreateChangeAndCommit("branch1 change", "b1")
		require.NoError(t, err)

		// Create branch2 on top of branch1
		err = scene.Repo.CreateAndCheckoutBranch("branch2")
		require.NoError(t, err)
		err = scene.Repo.CreateChangeAndCommit("branch2 change", "b2")
		require.NoError(t, err)

		// Get the base revision (branch1's initial commit)
		branch1Rev, err := scene.Repo.GetRef("branch1")
		require.NoError(t, err)

		// Add commit to main (parent moves forward)
		err = scene.Repo.CheckoutBranch("main")
		require.NoError(t, err)
		err = scene.Repo.CreateChangeAndCommit("main update", "main")
		require.NoError(t, err)

		// Rebase branch1 onto new main
		result, err := git.Rebase(context.Background(), "branch1", "main", branch1Rev)
		require.NoError(t, err)
		require.Equal(t, git.RebaseDone, result)

		// Verify branch1 is now based on new main
		err = scene.Repo.CheckoutBranch("branch1")
		require.NoError(t, err)
		// branch1 should have the new main commit in its history
		commits, err := scene.Repo.ListCurrentBranchCommitMessages()
		require.NoError(t, err)
		require.Contains(t, commits, "main update")
	})

	t.Run("handles rebase conflict", func(t *testing.T) {
		scene := testhelpers.NewScene(t, func(s *testhelpers.Scene) error {
			// Create initial file that will be modified to create conflict
			return s.Repo.CreateChangeAndCommit("initial content", "conflict")
		})

		// Get the fork point (main's current SHA before branching)
		forkPoint, err := scene.Repo.GetRef("main")
		require.NoError(t, err)

		// Create branch1 with change to the SAME file
		err = scene.Repo.CreateAndCheckoutBranch("branch1")
		require.NoError(t, err)
		err = scene.Repo.CreateChange("branch1 modification", "conflict", false)
		require.NoError(t, err)
		err = scene.Repo.CreateChangeAndCommit("branch1 change", "b1")
		require.NoError(t, err)

		// Create conflicting change in main (modifying the same file)
		err = scene.Repo.CheckoutBranch("main")
		require.NoError(t, err)
		err = scene.Repo.CreateChange("main conflicting modification", "conflict", false)
		require.NoError(t, err)
		err = scene.Repo.CreateChangeAndCommit("main conflicting change", "main")
		require.NoError(t, err)

		// Rebase should result in conflict (using fork point, not branch tip)
		result, err := git.Rebase(context.Background(), "branch1", "main", forkPoint)
		require.NoError(t, err)
		require.Equal(t, git.RebaseConflict, result)

		// Verify rebase is in progress
		require.True(t, git.IsRebaseInProgress(context.Background()))
	})
}

func TestIsRebaseInProgress(t *testing.T) {
	t.Run("returns false when no rebase", func(t *testing.T) {
		_ = testhelpers.NewScene(t, func(s *testhelpers.Scene) error {
			return s.Repo.CreateChangeAndCommit("initial", "init")
		})

		require.False(t, git.IsRebaseInProgress(context.Background()))
	})

	t.Run("returns true when rebase in progress", func(t *testing.T) {
		scene := testhelpers.NewScene(t, func(s *testhelpers.Scene) error {
			// Create initial file that will be modified to create conflict
			return s.Repo.CreateChangeAndCommit("initial content", "conflict")
		})

		// Get the fork point before branching
		forkPoint, err := scene.Repo.GetRef("main")
		require.NoError(t, err)

		// Create branch and conflict scenario
		err = scene.Repo.CreateAndCheckoutBranch("branch1")
		require.NoError(t, err)
		err = scene.Repo.CreateChange("branch1 change", "conflict", false)
		require.NoError(t, err)
		err = scene.Repo.CreateChangeAndCommit("branch1 change", "b1")
		require.NoError(t, err)

		err = scene.Repo.CheckoutBranch("main")
		require.NoError(t, err)
		err = scene.Repo.CreateChange("main conflicting", "conflict", false)
		require.NoError(t, err)
		err = scene.Repo.CreateChangeAndCommit("main conflicting", "main")
		require.NoError(t, err)

		// Start rebase (will conflict)
		_, err = git.Rebase(context.Background(), "branch1", "main", forkPoint)
		require.NoError(t, err)

		// Rebase should be in progress
		require.True(t, git.IsRebaseInProgress(context.Background()))
	})
}

func TestRebaseContinue(t *testing.T) {
	t.Run("continues rebase after resolving conflict", func(t *testing.T) {
		scene := testhelpers.NewScene(t, func(s *testhelpers.Scene) error {
			// Create initial file that will be modified to create conflict
			return s.Repo.CreateChangeAndCommit("initial content", "conflict")
		})

		// Get the fork point before branching
		forkPoint, err := scene.Repo.GetRef("main")
		require.NoError(t, err)

		// Create branch with conflict
		err = scene.Repo.CreateAndCheckoutBranch("branch1")
		require.NoError(t, err)
		err = scene.Repo.CreateChange("branch1 change", "conflict", false)
		require.NoError(t, err)
		err = scene.Repo.CreateChangeAndCommit("branch1 change", "b1")
		require.NoError(t, err)

		err = scene.Repo.CheckoutBranch("main")
		require.NoError(t, err)
		err = scene.Repo.CreateChange("main conflicting", "conflict", false)
		require.NoError(t, err)
		err = scene.Repo.CreateChangeAndCommit("main conflicting", "main")
		require.NoError(t, err)

		// Start rebase (will conflict)
		_, err = git.Rebase(context.Background(), "branch1", "main", forkPoint)
		require.NoError(t, err)
		require.True(t, git.IsRebaseInProgress(context.Background()))

		// Resolve conflict
		err = scene.Repo.ResolveMergeConflicts()
		require.NoError(t, err)
		err = scene.Repo.MarkMergeConflictsAsResolved()
		require.NoError(t, err)

		// Continue rebase
		result, err := git.RebaseContinue(context.Background())
		require.NoError(t, err)
		require.Equal(t, git.RebaseDone, result)

		// Rebase should be complete
		require.False(t, git.IsRebaseInProgress(context.Background()))
	})
}

func TestGetRebaseHead(t *testing.T) {
	t.Run("returns rebase head when rebase in progress", func(t *testing.T) {
		scene := testhelpers.NewScene(t, func(s *testhelpers.Scene) error {
			// Create initial file that will be modified to create conflict
			return s.Repo.CreateChangeAndCommit("initial content", "conflict")
		})

		// Get the fork point before branching
		forkPoint, err := scene.Repo.GetRef("main")
		require.NoError(t, err)

		// Create conflict scenario
		err = scene.Repo.CreateAndCheckoutBranch("branch1")
		require.NoError(t, err)
		err = scene.Repo.CreateChange("branch1 change", "conflict", false)
		require.NoError(t, err)
		err = scene.Repo.CreateChangeAndCommit("branch1 change", "b1")
		require.NoError(t, err)

		err = scene.Repo.CheckoutBranch("main")
		require.NoError(t, err)
		err = scene.Repo.CreateChange("main conflicting", "conflict", false)
		require.NoError(t, err)
		err = scene.Repo.CreateChangeAndCommit("main conflicting", "main")
		require.NoError(t, err)

		// Start rebase (will conflict)
		_, err = git.Rebase(context.Background(), "branch1", "main", forkPoint)
		require.NoError(t, err)

		// Verify we're in a conflict state
		require.True(t, git.IsRebaseInProgress(context.Background()))

		// Initialize default repo for GetRebaseHead
		git.SetWorkingDir(scene.Repo.Dir)
		err = git.InitDefaultRepoInDir(scene.Dir)
		require.NoError(t, err)

		// Get rebase head
		rebaseHead, err := git.GetRebaseHead()
		require.NoError(t, err)
		require.NotEmpty(t, rebaseHead)
	})
}
