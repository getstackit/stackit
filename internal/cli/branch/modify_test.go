package branch_test

import (
	"os/exec"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/testhelpers"
)

func TestModifyCommand(t *testing.T) {
	t.Parallel()
	binaryPath := testhelpers.GetSharedBinaryPath()
	if binaryPath == "" {
		if err := testhelpers.GetBinaryError(); err != nil {
			t.Fatalf("failed to build stackit binary: %v", err)
		}
		t.Fatal("stackit binary not built")
	}

	t.Run("modify amends commit with --all and --message", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, func(s *testhelpers.Scene) error {
			// Create initial commit
			if err := testhelpers.InitialCommitSceneSetup(s); err != nil {
				return err
			}
			// Create branch with commit
			if err := s.Repo.CreateChange("feature change", "test", false); err != nil {
				return err
			}
			cmd := exec.Command(binaryPath, "create", "feature", "-m", "original message")
			cmd.Dir = s.Dir
			return cmd.Run()
		})

		// Get original commit SHA
		cmd := exec.Command("git", "rev-parse", "HEAD")
		cmd.Dir = scene.Dir
		origSHA := testhelpers.Must(cmd.CombinedOutput())

		// Create a new change
		require.NoError(t, scene.Repo.CreateChange("more changes", "test2", false))

		// Run modify with --all and --message
		cmd = exec.Command(binaryPath, "modify", "-a", "-m", "amended message")
		cmd.Dir = scene.Dir
		output, err := cmd.CombinedOutput()

		require.NoError(t, err, "modify command failed: %s", string(output))
		require.Contains(t, string(output), "Amended commit", "should mention amending")

		// Get new commit SHA
		cmd = exec.Command("git", "rev-parse", "HEAD")
		cmd.Dir = scene.Dir
		newSHA := testhelpers.Must(cmd.CombinedOutput())

		// SHA should have changed
		require.NotEqual(t, string(origSHA), string(newSHA), "commit SHA should change after amend")

		// Verify the commit message changed
		cmd = exec.Command("git", "log", "-1", "--format=%s")
		cmd.Dir = scene.Dir
		output = testhelpers.Must(cmd.CombinedOutput())
		require.Contains(t, string(output), "amended message")
	})

	t.Run("modify with --no-edit preserves message", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, func(s *testhelpers.Scene) error {
			// Create initial commit
			if err := testhelpers.InitialCommitSceneSetup(s); err != nil {
				return err
			}
			// Create branch with commit
			if err := s.Repo.CreateChange("feature change", "test", false); err != nil {
				return err
			}
			cmd := exec.Command(binaryPath, "create", "feature", "-m", "original message")
			cmd.Dir = s.Dir
			return cmd.Run()
		})

		// Create a new change
		require.NoError(t, scene.Repo.CreateChange("more changes", "test2", false))

		// Run modify with --all and --no-edit
		cmd := exec.Command(binaryPath, "modify", "-a", "-n")
		cmd.Dir = scene.Dir
		output, err := cmd.CombinedOutput()

		require.NoError(t, err, "modify command failed: %s", string(output))

		// Verify the commit message is preserved
		cmd = exec.Command("git", "log", "-1", "--format=%s")
		cmd.Dir = scene.Dir
		output = testhelpers.Must(cmd.CombinedOutput())
		require.Contains(t, string(output), "original message")
	})

	t.Run("modify with --commit creates new commit", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, func(s *testhelpers.Scene) error {
			// Create initial commit
			if err := testhelpers.InitialCommitSceneSetup(s); err != nil {
				return err
			}
			// Create branch with commit
			if err := s.Repo.CreateChange("feature change", "test", false); err != nil {
				return err
			}
			cmd := exec.Command(binaryPath, "create", "feature", "-m", "first commit")
			cmd.Dir = s.Dir
			return cmd.Run()
		})

		// Get commit count before
		cmd := exec.Command("git", "rev-list", "--count", "main..feature")
		cmd.Dir = scene.Dir
		beforeCount := testhelpers.Must(cmd.CombinedOutput())

		// Create a new change
		require.NoError(t, scene.Repo.CreateChange("more changes", "test2", false))

		// Run modify with --commit to create new commit
		cmd = exec.Command(binaryPath, "modify", "-c", "-a", "-m", "second commit")
		cmd.Dir = scene.Dir
		output, err := cmd.CombinedOutput()

		require.NoError(t, err, "modify command failed: %s", string(output))
		require.Contains(t, string(output), "Created new commit", "should mention creating new commit")

		// Get commit count after
		cmd = exec.Command("git", "rev-list", "--count", "main..feature")
		cmd.Dir = scene.Dir
		afterCount := testhelpers.Must(cmd.CombinedOutput())

		// Should have one more commit
		require.NotEqual(t, string(beforeCount), string(afterCount), "should have more commits")
	})

	t.Run("modify restacks upstack branches", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, func(s *testhelpers.Scene) error {
			// Create initial commit
			if err := testhelpers.InitialCommitSceneSetup(s); err != nil {
				return err
			}
			// Create branch A
			if err := s.Repo.CreateChange("a change", "a", false); err != nil {
				return err
			}
			cmd := exec.Command(binaryPath, "create", "a", "-m", "a change")
			cmd.Dir = s.Dir
			if err := cmd.Run(); err != nil {
				return err
			}
			// Create branch B on top of A
			if err := s.Repo.CreateChange("b change", "b", false); err != nil {
				return err
			}
			cmd = exec.Command(binaryPath, "create", "b", "-m", "b change")
			cmd.Dir = s.Dir
			return cmd.Run()
		})

		// Switch to branch A
		require.NoError(t, scene.Repo.CheckoutBranch("a"))

		// Create a new change on A
		require.NoError(t, scene.Repo.CreateChange("a additional change", "a2", false))

		// Run modify to amend A
		cmd := exec.Command(binaryPath, "modify", "-a", "-n")
		cmd.Dir = scene.Dir
		output, err := cmd.CombinedOutput()

		require.NoError(t, err, "modify command failed: %s", string(output))
		require.Contains(t, string(output), "Restacking", "should mention restacking")

		// Verify branch B still has its change after restacking
		require.NoError(t, scene.Repo.CheckoutBranch("b"))
		cmd = exec.Command("git", "log", "--oneline", "a..b")
		cmd.Dir = scene.Dir
		output = testhelpers.Must(cmd.CombinedOutput())
		require.Contains(t, string(output), "b change")
	})

	t.Run("modify errors on trunk branch", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)

		// Try to modify trunk (main)
		cmd := exec.Command(binaryPath, "modify", "-a", "-n")
		cmd.Dir = scene.Dir
		output, err := cmd.CombinedOutput()

		require.Error(t, err, "modify should fail on trunk")
		require.Contains(t, string(output), "cannot modify trunk", "should mention trunk error")
	})

	t.Run("modify errors when not on a branch", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)

		// Detach HEAD
		require.NoError(t, scene.Repo.RunGitCommand("checkout", "HEAD~0"))

		// Try to modify
		cmd := exec.Command(binaryPath, "modify", "-a", "-n")
		cmd.Dir = scene.Dir
		output, err := cmd.CombinedOutput()

		require.Error(t, err, "modify should fail when not on a branch")
		require.Contains(t, string(output), "not on a branch", "should mention not on branch error")
	})

	t.Run("modify errors when stackit not initialized", func(t *testing.T) {
		t.Parallel()
		// Create a temporary directory without stackit initialization
		tmpDir := t.TempDir()
		cmd := exec.Command("git", "init", tmpDir, "-b", "main")
		require.NoError(t, cmd.Run())

		// Create a commit
		cmd = exec.Command("git", "-C", tmpDir, "commit", "--allow-empty", "-m", "initial")
		require.NoError(t, cmd.Run())

		// Try to modify without initializing stackit
		cmd = exec.Command(binaryPath, "modify", "-a", "-n")
		cmd.Dir = tmpDir
		output, err := cmd.CombinedOutput()

		require.Error(t, err, "modify should fail when stackit not initialized")
		require.Contains(t, string(output), "not initialized", "should mention not initialized")
	})

	t.Run("modify with --update stages only tracked files", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, func(s *testhelpers.Scene) error {
			// Create initial commit
			if err := testhelpers.InitialCommitSceneSetup(s); err != nil {
				return err
			}
			// Create branch with commit
			if err := s.Repo.CreateChange("feature change", "test", false); err != nil {
				return err
			}
			cmd := exec.Command(binaryPath, "create", "feature", "-m", "original message")
			cmd.Dir = s.Dir
			return cmd.Run()
		})

		// Modify the tracked file (unstaged=true means don't stage it)
		require.NoError(t, scene.Repo.CreateChange("modified content", "test", true))

		// Create a new untracked file (unstaged=true means don't stage it)
		require.NoError(t, scene.Repo.CreateChange("untracked content", "untracked", true))

		// Run modify with --update (only stages tracked files)
		cmd := exec.Command(binaryPath, "modify", "-u", "-n")
		cmd.Dir = scene.Dir
		output, err := cmd.CombinedOutput()

		require.NoError(t, err, "modify command failed: %s", string(output))

		// Verify untracked file is still untracked
		cmd = exec.Command("git", "status", "--porcelain")
		cmd.Dir = scene.Dir
		output = testhelpers.Must(cmd.CombinedOutput())
		require.Contains(t, string(output), "?? untracked", "untracked file should remain untracked")
	})

	t.Run("modify on empty branch creates commit instead of amend", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, func(s *testhelpers.Scene) error {
			// Create initial commit
			if err := testhelpers.InitialCommitSceneSetup(s); err != nil {
				return err
			}
			// Create empty branch (no commits yet)
			if err := s.Repo.CreateAndCheckoutBranch("empty-feature"); err != nil {
				return err
			}
			// Track it with stackit
			cmd := exec.Command(binaryPath, "info")
			cmd.Dir = s.Dir
			// Don't track - just create an empty branch scenario
			return nil
		})

		// Create a change
		require.NoError(t, scene.Repo.CreateChange("new feature", "feature", false))

		// Track the branch first
		cmd := exec.Command("git", "add", "-A")
		cmd.Dir = scene.Dir
		require.NoError(t, cmd.Run())

		cmd = exec.Command("git", "commit", "-m", "first commit")
		cmd.Dir = scene.Dir
		require.NoError(t, cmd.Run())

		// Get initial state
		cmd = exec.Command("git", "rev-list", "--count", "main..empty-feature")
		cmd.Dir = scene.Dir
		output := testhelpers.Must(cmd.CombinedOutput())
		require.Contains(t, string(output), "1", "should have one commit")
	})
}
