package branch_test

import (
	"os/exec"
	"testing"

	"github.com/stretchr/testify/require"

	"stackit.dev/stackit/testhelpers"
)

func TestGetCommand(t *testing.T) {
	t.Parallel()
	binaryPath := testhelpers.GetSharedBinaryPath()
	if binaryPath == "" {
		if err := testhelpers.GetBinaryError(); err != nil {
			t.Fatalf("failed to build stackit binary: %v", err)
		}
		t.Fatal("stackit binary not built")
	}

	t.Run("get from remote shows fetch and sync output", func(t *testing.T) {
		t.Parallel()
		remoteDir := t.TempDir()
		scene := testhelpers.NewSceneParallel(t, func(s *testhelpers.Scene) error {
			// Create initial commit
			if err := testhelpers.InitialCommitSceneSetup(s); err != nil {
				return err
			}
			// Initialize stackit
			cmd := exec.Command(binaryPath, "init")
			cmd.Dir = s.Dir
			if err := cmd.Run(); err != nil {
				return err
			}
			// Create a remote (bare repo)
			cmd = exec.Command("git", "init", "--bare", remoteDir)
			if err := cmd.Run(); err != nil {
				return err
			}
			cmd = exec.Command("git", "remote", "add", "origin", remoteDir)
			cmd.Dir = s.Dir
			if err := cmd.Run(); err != nil {
				return err
			}
			cmd = exec.Command("git", "push", "-u", "origin", "main")
			cmd.Dir = s.Dir
			if err := cmd.Run(); err != nil {
				return err
			}
			// Create a branch and push it
			if err := s.Repo.CreateChangeAndCommit("feature change", "feature"); err != nil {
				return err
			}
			cmd = exec.Command(binaryPath, "create", "feature-branch", "-m", "feature change")
			cmd.Dir = s.Dir
			if err := cmd.Run(); err != nil {
				return err
			}
			cmd = exec.Command("git", "push", "-u", "origin", "feature-branch")
			cmd.Dir = s.Dir
			if err := cmd.Run(); err != nil {
				return err
			}
			// Remove local branch
			cmd = exec.Command("git", "checkout", "main")
			cmd.Dir = s.Dir
			if err := cmd.Run(); err != nil {
				return err
			}
			cmd = exec.Command("git", "branch", "-D", "feature-branch")
			cmd.Dir = s.Dir
			if err := cmd.Run(); err != nil {
				return err
			}
			// Delete stackit tracking
			cmd = exec.Command(binaryPath, "untrack", "feature-branch")
			cmd.Dir = s.Dir
			// Ignore error as branch may not be tracked
			_ = cmd.Run()
			return nil
		})

		// Run get
		cmd := exec.Command(binaryPath, "get", "feature-branch", "--no-restack")
		cmd.Dir = scene.Dir
		output, err := cmd.CombinedOutput()

		require.NoError(t, err, "get command failed: %s", string(output))

		normalized := testhelpers.NormalizeOutput(string(output))
		require.Contains(t, normalized, "Fetching from remote", "should show fetch phase")
		require.Contains(t, normalized, "Syncing branches", "should show sync phase")
		require.Contains(t, normalized, "Synced feature-branch", "should show branch synced")
		require.Contains(t, normalized, "Checked out feature-branch", "should show checkout")
		require.Contains(t, normalized, "frozen", "should show frozen mode message")
	})

	t.Run("get fails with uncommitted changes", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, func(s *testhelpers.Scene) error {
			// Create initial commit
			if err := testhelpers.InitialCommitSceneSetup(s); err != nil {
				return err
			}
			// Initialize stackit
			cmd := exec.Command(binaryPath, "init")
			cmd.Dir = s.Dir
			return cmd.Run()
		})

		// Create an uncommitted change
		err := scene.Repo.CreateChange("uncommitted", "uncommitted", false)
		require.NoError(t, err)

		// Run get
		cmd := exec.Command(binaryPath, "get", "some-branch")
		cmd.Dir = scene.Dir
		output, err := cmd.CombinedOutput()

		require.Error(t, err, "get should fail with uncommitted changes")
		require.Contains(t, string(output), "uncommitted changes", "should mention uncommitted changes")
	})

	t.Run("get with --unfrozen flag", func(t *testing.T) {
		t.Parallel()
		remoteDir := t.TempDir()
		scene := testhelpers.NewSceneParallel(t, func(s *testhelpers.Scene) error {
			// Create initial commit
			if err := testhelpers.InitialCommitSceneSetup(s); err != nil {
				return err
			}
			// Initialize stackit
			cmd := exec.Command(binaryPath, "init")
			cmd.Dir = s.Dir
			if err := cmd.Run(); err != nil {
				return err
			}
			// Create a remote (bare repo)
			cmd = exec.Command("git", "init", "--bare", remoteDir)
			if err := cmd.Run(); err != nil {
				return err
			}
			cmd = exec.Command("git", "remote", "add", "origin", remoteDir)
			cmd.Dir = s.Dir
			if err := cmd.Run(); err != nil {
				return err
			}
			cmd = exec.Command("git", "push", "-u", "origin", "main")
			cmd.Dir = s.Dir
			if err := cmd.Run(); err != nil {
				return err
			}
			// Create a branch and push it
			if err := s.Repo.CreateChangeAndCommit("feature change", "feature"); err != nil {
				return err
			}
			cmd = exec.Command(binaryPath, "create", "feature-branch", "-m", "feature change")
			cmd.Dir = s.Dir
			if err := cmd.Run(); err != nil {
				return err
			}
			cmd = exec.Command("git", "push", "-u", "origin", "feature-branch")
			cmd.Dir = s.Dir
			if err := cmd.Run(); err != nil {
				return err
			}
			// Remove local branch
			cmd = exec.Command("git", "checkout", "main")
			cmd.Dir = s.Dir
			if err := cmd.Run(); err != nil {
				return err
			}
			cmd = exec.Command("git", "branch", "-D", "feature-branch")
			cmd.Dir = s.Dir
			if err := cmd.Run(); err != nil {
				return err
			}
			return nil
		})

		// Run get with --unfrozen
		cmd := exec.Command(binaryPath, "get", "feature-branch", "--unfrozen", "--no-restack")
		cmd.Dir = scene.Dir
		output, err := cmd.CombinedOutput()

		require.NoError(t, err, "get command failed: %s", string(output))

		normalized := testhelpers.NormalizeOutput(string(output))
		require.Contains(t, normalized, "Checked out feature-branch", "should show checkout")
		// With --unfrozen, should NOT show frozen message
		require.NotContains(t, normalized, "frozen", "should not show frozen mode message when using --unfrozen")
	})
}
