package navigation_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"stackit.dev/stackit/testhelpers"
	"stackit.dev/stackit/testhelpers/scenario"
)

func TestCheckoutCommand(t *testing.T) {
	t.Parallel()
	binaryPath := testhelpers.GetSharedBinaryPath()
	if binaryPath == "" {
		if err := testhelpers.GetBinaryError(); err != nil {
			t.Fatalf("failed to build stackit binary: %v", err)
		}
		t.Fatal("stackit binary not built")
	}

	t.Run("successful navigation", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenarioParallel(t, testhelpers.BasicSceneSetup).WithBinaryPath(binaryPath)

		// Create a stack: main -> a -> b
		s.RunCli("create", "a", "-m", "a").
			RunCli("create", "b", "-m", "b")

		// 1. Direct checkout
		output, err := s.RunCliAndGetOutput("checkout", "a")
		require.NoError(t, err)
		require.Contains(t, testhelpers.NormalizeOutput(output), "Checked out a.")
		s.ExpectBranch("a")

		// 2. Trunk flag
		output, err = s.RunCliAndGetOutput("checkout", "--trunk")
		require.NoError(t, err)
		require.Contains(t, testhelpers.NormalizeOutput(output), "Checked out main.")
		s.ExpectBranch("main")

		// 3. Trunk short flag
		s.RunCli("checkout", "a")
		output, err = s.RunCliAndGetOutput("checkout", "-t")
		require.NoError(t, err)
		require.Contains(t, testhelpers.NormalizeOutput(output), "Checked out main.")
		s.ExpectBranch("main")

		// 4. Alias co
		output, err = s.RunCliAndGetOutput("co", "a")
		require.NoError(t, err)
		require.Contains(t, testhelpers.NormalizeOutput(output), "Checked out a.")
		s.ExpectBranch("a")

		// 5. Already on branch
		output, err = s.RunCliAndGetOutput("checkout", "a")
		require.NoError(t, err)
		require.Contains(t, testhelpers.NormalizeOutput(output), "Already on a (current).")
	})

	t.Run("failure and interactive flags", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenarioParallel(t, testhelpers.BasicSceneSetup).WithBinaryPath(binaryPath)

		// 1. Invalid branch
		output, err := s.RunCliAndGetOutput("checkout", "nonexistent")
		require.Error(t, err)
		require.Contains(t, testhelpers.NormalizeOutput(output), "failed to checkout")

		// 2. Interactive flag in non-interactive mode
		s.RunCli("create", "a")
		output, err = s.RunCliAndGetOutput("checkout", "--stack")
		require.Error(t, err)
		require.Contains(t, testhelpers.NormalizeOutput(output), "interactive")
	})
}
