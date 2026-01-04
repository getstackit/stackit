package stack_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"stackit.dev/stackit/testhelpers"
	"stackit.dev/stackit/testhelpers/scenario"
)

func TestSyncCommand(t *testing.T) {
	t.Parallel()
	binaryPath := testhelpers.GetSharedBinaryPath()
	if binaryPath == "" {
		if err := testhelpers.GetBinaryError(); err != nil {
			t.Fatalf("failed to build stackit binary: %v", err)
		}
		t.Fatal("stackit binary not built")
	}

	t.Run("successful sync scenarios", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenarioParallel(t, testhelpers.BasicSceneSetup).WithBinaryPath(binaryPath)

		// Create a remote to avoid sync errors related to missing remote
		_, err := s.Scene.Repo.CreateBareRemote("origin")
		require.NoError(t, err)

		s.RunCli("init")
		s.RunCli("create", "branch1", "-m", "branch1")
		// Add a commit to branch1 so it's not empty and doesn't get cleaned up by sync
		s.RunGit("commit", "--allow-empty", "-m", "work on branch1")

		// 1. Trunk up to date
		output, err := s.RunCliAndGetOutput("sync", "--no-restack")
		require.NoError(t, err, "sync --no-restack failed: %s", output)
		normalized := testhelpers.NormalizeOutput(output)
		require.Equal(t, testhelpers.NormalizeOutput(`
📥 Pulling from remote...
  main is up to date
🔄 Fetching PR info from GitHub...
  PR info up to date
🧹 Cleaning branches...
💡 Try the --restack flag to automatically restack the current stack.
✨ Everything is up to date!
`), normalized)

		// 2. Restack not needed
		output, err = s.RunCliAndGetOutput("sync", "--restack")
		require.NoError(t, err, "sync --restack (not needed) failed: %s", output)
		normalized = testhelpers.NormalizeOutput(output)
		require.Equal(t, testhelpers.NormalizeOutput(`
📥 Pulling from remote...
  main is up to date
🔄 Fetching PR info from GitHub...
  PR info up to date
🧹 Cleaning branches...
📚 Restacking branches...
  branch1 does not need restacking
✨ Everything is up to date!
`), normalized)

		// 3. Restack needed
		s.RunGit("checkout", "main")
		s.Scene.Repo.CreateChangeAndCommit("main update", "main-file")
		s.RunCli("checkout", "branch1")

		output, err = s.RunCliAndGetOutput("sync", "--restack")
		require.NoError(t, err, "sync --restack (needed) failed: %s", output)
		normalized = testhelpers.NormalizeOutput(output)
		// We don't know the exact revision, so we'll check the structure
		require.Contains(t, normalized, "Restacked branch1 ->")
		require.Contains(t, normalized, "✅ Summary: restacked 1")
	})

	t.Run("sync failures and tips", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenarioParallel(t, testhelpers.BasicSceneSetup).WithBinaryPath(binaryPath)

		// Create a remote to avoid sync errors related to missing remote
		_, err := s.Scene.Repo.CreateBareRemote("origin")
		require.NoError(t, err)

		s.RunCli("init")

		// 1. Uncommitted changes failure
		s.WithUncommittedChange("unstaged")
		output, err := s.RunCliAndGetOutput("sync")
		require.Error(t, err)
		require.Equal(t, testhelpers.NormalizeOutput(`
Error: you have uncommitted changes. Please commit or stash them before syncing
`), testhelpers.NormalizeOutput(output))

		// 2. Reset and check tip
		s.RunGit("reset", "--hard")
		s.RunGit("clean", "-fd") // Ensure untracked files are also gone

		output, err = s.RunCliAndGetOutput("sync", "--no-restack")
		require.NoError(t, err, "sync --no-restack failed: %s", output)
		require.Equal(t, testhelpers.NormalizeOutput(`
📥 Pulling from remote...
  main is up to date
🔄 Fetching PR info from GitHub...
  PR info up to date
💡 Try the --restack flag to automatically restack the current stack.
✨ Everything is up to date!
`), testhelpers.NormalizeOutput(output))
	})
}
