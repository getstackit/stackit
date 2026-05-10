package navigation_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

func TestLogCommand(t *testing.T) {
	t.Parallel()
	// Build the stackit binary first
	binaryPath := testhelpers.GetSharedBinaryPath()
	if binaryPath == "" {
		if err := testhelpers.GetBinaryError(); err != nil {
			t.Fatalf("failed to build stackit binary: %v", err)
		}
		t.Fatal("stackit binary not built")
	}

	t.Run("log in empty repo", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenarioParallel(t, testhelpers.BasicSceneSetup).WithBinaryPath(binaryPath)

		// Run log command
		output, err := s.RunCliAndGetOutput("log")

		// Should succeed and show trunk branch
		require.NoError(t, err, "log command failed: %s", output)
		require.Contains(t, output, "main")
	})

	t.Run("log with branches", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenarioParallel(t, testhelpers.BasicSceneSetup).WithBinaryPath(binaryPath)

		// Create a branch
		s.CreateBranch("feature").
			CommitChange("feature", "feature commit")

		// Checkout main
		s.Checkout("main")

		// Run log command with --show-untracked to see untracked branches
		output, err := s.RunCliAndGetOutput("log", "--show-untracked")

		require.NoError(t, err, "log command failed: %s", output)
		require.Contains(t, output, "main")
		require.Contains(t, output, "feature")
	})

	t.Run("log with --stack flag", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenarioParallel(t, testhelpers.BasicSceneSetup).WithBinaryPath(binaryPath)

		// Create and checkout a branch
		s.CreateBranch("feature")

		// Run log command with stack
		output, err := s.RunCliAndGetOutput("log", "--stack")

		require.NoError(t, err, "log command failed: %s", output)
		require.Contains(t, output, "feature")
	})

	t.Run("log shows worktree indicator for stack with worktree", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenarioParallel(t, testhelpers.BasicSceneSetup).WithBinaryPath(binaryPath)
		s.WithInitialCommit()

		// Create a staged change for the branch
		s.CommitChange("feature-file", "feature content")

		// Go back to main to create branch with worktree
		s.Checkout("main")

		// Stage a change for the worktree branch
		err := s.Scene.Repo.CreateChange("worktree-content", "worktree-file", false)
		require.NoError(t, err)

		// Create branch with worktree using CLI
		output, err := s.RunCliAndGetOutput("create", "-m", "worktree feature", "-w")
		require.NoError(t, err, "create with worktree failed: %s", output)

		// Run log command - should show worktree indicator
		output, err = s.RunCliAndGetOutput("log")
		require.NoError(t, err, "log command failed: %s", output)
		require.Contains(t, output, "worktree", "log should show worktree indicator for branch with managed worktree")
	})
}
