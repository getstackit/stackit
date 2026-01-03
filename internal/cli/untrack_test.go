package cli_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"stackit.dev/stackit/testhelpers"
	"stackit.dev/stackit/testhelpers/scenario"
)

func TestUntrackCommand(t *testing.T) {
	t.Parallel()

	t.Run("untrack current branch", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, func(sc *testhelpers.Scene) error {
			return sc.Repo.CreateChangeAndCommit("initial", "init")
		}).WithInProcess(true)

		// Create a tracked branch
		s.RunCli("create", "a", "-m", "Add a")

		// Untrack current branch (a)
		output, err := s.RunCliAndGetOutput("untrack")
		require.NoError(t, err, "untrack command failed: %s", output)
		require.Contains(t, output, "Stopped tracking a")

		// Verify branch is no longer tracked
		output, err = s.RunCliAndGetOutput("parent")
		require.NoError(t, err)
		require.Contains(t, output, "has no parent (untracked branch)")
	})

	t.Run("untrack specified branch", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, func(sc *testhelpers.Scene) error {
			return sc.Repo.CreateChangeAndCommit("initial", "init")
		}).WithInProcess(true)

		// Create tracked branches
		s.RunCli("create", "a", "-m", "Add a")
		s.RunCli("create", "b", "-m", "Add b")

		// Untrack branch a while on branch b
		output, err := s.RunCliAndGetOutput("untrack", "a", "--force")
		require.NoError(t, err, "untrack command failed: %s", output)
		require.Contains(t, output, "Stopped tracking a")
		require.Contains(t, output, "Stopped tracking b")

		// Verify branch a is no longer tracked
		s.RunCli("checkout", "a")
		output, err = s.RunCliAndGetOutput("parent")
		require.NoError(t, err)
		require.Contains(t, output, "has no parent (untracked branch)")

		// Verify branch b is also no longer tracked (it was a child of a)
		s.RunCli("checkout", "b")
		output, err = s.RunCliAndGetOutput("parent")
		require.NoError(t, err)
		require.Contains(t, output, "has no parent (untracked branch)")
	})

	t.Run("untrack fails for untracked branch", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, func(sc *testhelpers.Scene) error {
			return sc.Repo.CreateChangeAndCommit("initial", "init")
		}).WithInProcess(true)

		// Create an untracked branch
		err := s.Scene.Repo.CreateAndCheckoutBranch("untracked")
		require.NoError(t, err)

		// Try to untrack it
		output, err := s.RunCliAndGetOutput("untrack")
		require.Error(t, err)
		require.Contains(t, output, "branch untracked is not tracked")
	})
}
