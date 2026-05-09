package testhelpers_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"stackit.dev/stackit/testhelpers"
)

// TestExampleUsage demonstrates how to use the testhelpers package.
// This test shows the basic pattern for using scenes.
func TestExampleUsage(t *testing.T) {
	// Create a basic scene with a single commit
	scene := testhelpers.NewScene(t, testhelpers.BasicSceneSetup)

	// Verify initial state
	branches, err := scene.Repo.RunGitCommandAndGetOutput("branch", "--list")
	require.NoError(t, err)
	require.Contains(t, branches, "main")

	// Example: Create a branch (once CLI is implemented)
	// err := scene.Repo.RunCliCommand([]string{"branch", "create", "feature"})
	// require.NoError(t, err)
	// testhelpers.ExpectBranchesString(t, scene.Repo, "feature, main")
}

// TestGitRepoBasicOperations tests basic Git repository operations.
func TestGitRepoBasicOperations(t *testing.T) {
	scene := testhelpers.NewScene(t, nil)

	// Test creating a commit
	err := scene.Repo.CreateChangeAndCommit("test content", "test")
	require.NoError(t, err)

	// Test getting current branch
	branch, err := scene.Repo.CurrentBranchName()
	require.NoError(t, err)
	require.Equal(t, "main", branch)

	// Test listing commits
	messages, err := scene.Repo.ListCurrentBranchCommitMessages()
	require.NoError(t, err)
	require.Greater(t, len(messages), 0)
}

// TestSceneWithSetup demonstrates using a custom setup function.
func TestSceneWithSetup(t *testing.T) {
	customSetup := func(scene *testhelpers.Scene) error {
		// Create multiple commits
		if err := scene.Repo.CreateChangeAndCommit("commit 1", "1"); err != nil {
			return err
		}
		if err := scene.Repo.CreateChangeAndCommit("commit 2", "2"); err != nil {
			return err
		}
		return nil
	}

	scene := testhelpers.NewScene(t, customSetup)

	// Verify commits were created
	messages, err := scene.Repo.ListCurrentBranchCommitMessages()
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(messages), 2)
}

// TestExpectBranches demonstrates the branch assertion helper.
func TestExpectBranches(t *testing.T) {
	scene := testhelpers.NewScene(t, testhelpers.InitialCommitSceneSetup)

	// Create branches manually
	err := scene.Repo.CreateAndCheckoutBranch("feature")
	require.NoError(t, err)
	err = scene.Repo.CreateAndCheckoutBranch("bugfix")
	require.NoError(t, err)
	err = scene.Repo.CheckoutBranch("main")
	require.NoError(t, err)

	// Assert branches exist
	testhelpers.ExpectBranches(t, scene.Repo, []string{"bugfix", "feature", "main"})
	testhelpers.ExpectBranchesString(t, scene.Repo, "bugfix, feature, main")
}
