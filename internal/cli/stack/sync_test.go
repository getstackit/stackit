package stack_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

func TestSyncCommand(t *testing.T) {
	t.Parallel()

	t.Run("successful sync scenarios", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenarioParallel(t, testhelpers.BasicSceneSetup).WithInProcess(true)

		// Create a remote to avoid sync errors related to missing remote
		_, err := s.Scene.Repo.CreateBareRemote("origin")
		require.NoError(t, err)
		err = s.Scene.Repo.PushBranch("origin", "main")
		require.NoError(t, err)

		s.RunCli("init")
		s.RunCli("create", "branch1", "-m", "branch1")
		// Add a commit to branch1 so it's not empty and doesn't get cleaned up by sync
		s.RunGit("commit", "--allow-empty", "-m", "work on branch1")

		// 1. Trunk up to date
		output, err := s.RunCliAndGetOutput("sync", "--no-restack")
		require.NoError(t, err, "sync --no-restack failed: %s", output)
		normalized := testhelpers.NormalizeOutput(output)
		// A run that changed nothing says exactly that and nothing else: every
		// phase reports only what it actually did.
		require.Equal(t, testhelpers.NormalizeOutput(`
Already up to date.
`), normalized)

		// 2. Restack not needed
		output, err = s.RunCliAndGetOutput("sync", "--restack")
		require.NoError(t, err, "sync --restack (not needed) failed: %s", output)
		normalized = testhelpers.NormalizeOutput(output)
		require.Equal(t, testhelpers.NormalizeOutput(`
Already up to date.
`), normalized)

		// 3. Restack needed
		s.RunGit("checkout", "main")
		s.Scene.Repo.CreateChangeAndCommit("main update", "main-file")
		s.RunCli("checkout", "branch1")

		output, err = s.RunCliAndGetOutput("sync", "--restack")
		require.NoError(t, err, "sync --restack (needed) failed: %s", output)
		normalized = testhelpers.NormalizeOutput(output)
		// We don't know the exact revision, so we'll check the structure
		require.Contains(t, normalized, "Restacked 1 branch")
		require.Contains(t, normalized, "Restacked 1")
	})

	t.Run("sync failures and tips", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenarioParallel(t, testhelpers.BasicSceneSetup).WithInProcess(true)

		// Create a remote to avoid sync errors related to missing remote
		_, err := s.Scene.Repo.CreateBareRemote("origin")
		require.NoError(t, err)
		err = s.Scene.Repo.PushBranch("origin", "main")
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
Already up to date.
`), testhelpers.NormalizeOutput(output))
	})

	t.Run("dry-run previews without mutating", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenarioParallel(t, testhelpers.BasicSceneSetup).WithInProcess(true)

		_, err := s.Scene.Repo.CreateBareRemote("origin")
		require.NoError(t, err)
		require.NoError(t, s.Scene.Repo.PushBranch("origin", "main"))

		s.RunCli("init")
		s.RunCli("create", "branch1", "-m", "branch1")
		s.RunGit("commit", "--allow-empty", "-m", "work on branch1")

		// Make branch1 need a restack by advancing main behind it.
		s.RunGit("checkout", "main")
		s.Scene.Repo.CreateChangeAndCommit("main update", "main-file")
		s.RunCli("checkout", "branch1")

		// --dry-run must PREVIEW the restack, not perform it.
		output, err := s.RunCliAndGetOutput("sync", "--dry-run", "--restack")
		require.NoError(t, err, "sync --dry-run failed: %s", output)
		normalized := testhelpers.NormalizeOutput(output)
		require.Contains(t, normalized, "Dry run")
		require.Contains(t, normalized, "Would restack")
		require.Contains(t, normalized, "branch1")

		// Proof it didn't mutate: the real sync still has the restack to do.
		// (If dry-run had restacked, this would report "up to date" instead.)
		output, err = s.RunCliAndGetOutput("sync", "--restack")
		require.NoError(t, err, "sync --restack failed: %s", output)
		require.Contains(t, testhelpers.NormalizeOutput(output), "Restacked 1 branch")
	})
}
