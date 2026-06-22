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

	t.Run("tree in empty repo", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenarioParallel(t, testhelpers.BasicSceneSetup).WithBinaryPath(binaryPath)

		// Run tree command
		output, err := s.RunCliAndGetOutput("tree")

		// Should succeed and show trunk branch
		require.NoError(t, err, "tree command failed: %s", output)
		require.Contains(t, output, "main")
	})

	t.Run("tree with branches", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenarioParallel(t, testhelpers.BasicSceneSetup).WithBinaryPath(binaryPath)

		// Create a branch
		s.CreateBranch("feature").
			CommitChange("feature", "feature commit")

		// Checkout main
		s.Checkout("main")

		// Run tree command with --show-untracked to see untracked branches
		output, err := s.RunCliAndGetOutput("tree", "--show-untracked")

		require.NoError(t, err, "tree command failed: %s", output)
		require.Contains(t, output, "main")
		require.Contains(t, output, "feature")
	})

	t.Run("tree with --stack flag", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenarioParallel(t, testhelpers.BasicSceneSetup).WithBinaryPath(binaryPath)

		// Create and checkout a branch
		s.CreateBranch("feature")

		// Run tree command with stack
		output, err := s.RunCliAndGetOutput("tree", "--stack")

		require.NoError(t, err, "tree command failed: %s", output)
		require.Contains(t, output, "feature")
	})

	t.Run("t aliases tree short", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenarioParallel(t, testhelpers.BasicSceneSetup).WithBinaryPath(binaryPath)
		s.CreateBranch("feature")

		shortOutput, err := s.RunCliAndGetOutput("tree", "short")
		require.NoError(t, err, "tree short command failed: %s", shortOutput)

		aliasOutput, err := s.RunCliAndGetOutput("t")
		require.NoError(t, err, "t command failed: %s", aliasOutput)

		require.Equal(t, shortOutput, aliasOutput)
		require.Contains(t, aliasOutput, "main")
	})

	t.Run("tree shows worktree indicator for stack with worktree", func(t *testing.T) {
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

		// Run tree command - should show worktree indicator
		output, err = s.RunCliAndGetOutput("tree")
		require.NoError(t, err, "tree command failed: %s", output)
		require.Contains(t, output, "worktree", "tree should show worktree indicator for branch with managed worktree")
	})
}
