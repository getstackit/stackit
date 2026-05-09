package branch_test

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"stackit.dev/stackit/testhelpers"
)

func TestSplitCommand(t *testing.T) {
	t.Parallel()
	binaryPath := testhelpers.GetSharedBinaryPath()
	if binaryPath == "" {
		if err := testhelpers.GetBinaryError(); err != nil {
			t.Fatalf("failed to build stackit binary: %v", err)
		}
		t.Fatal("stackit binary not built")
	}

	t.Run("split --by-file extracts files to new parent branch", func(t *testing.T) {
		t.Parallel()
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

			// Create a feature branch
			if err := s.Repo.CreateChange("feature change 1", "test1", false); err != nil {
				return err
			}
			cmd = exec.Command(binaryPath, "create", "feature", "-m", "feature change 1")
			cmd.Dir = s.Dir
			if err := cmd.Run(); err != nil {
				return err
			}

			// Add multiple files to the branch
			if err := s.Repo.CreateChange("file1 content", "file1", false); err != nil {
				return err
			}
			if err := s.Repo.CreateChange("file2 content", "file2", false); err != nil {
				return err
			}
			if err := s.Repo.CreateChange("file3 content", "file3", false); err != nil {
				return err
			}
			return s.Repo.CreateChangeAndCommit("Add multiple files", "Add multiple files")
		})

		// Verify we're on feature branch
		currentBranch, err := scene.Repo.CurrentBranchName()
		require.NoError(t, err)
		require.Equal(t, "feature", currentBranch)

		// Verify files exist on feature branch
		cmd := exec.Command("git", "ls-files", "file1_test.txt", "file2_test.txt", "file3_test.txt")
		cmd.Dir = scene.Dir
		output, err := cmd.Output()
		require.NoError(t, err)
		require.Contains(t, string(output), "file1_test.txt")
		require.Contains(t, string(output), "file2_test.txt")
		require.Contains(t, string(output), "file3_test.txt")

		// Run split --by-file to extract file1 and file2 (comma-separated for StringSlice flag)
		cmd = exec.Command(binaryPath, "split", "--by-file", "file1_test.txt,file2_test.txt", "--no-interactive")
		cmd.Dir = scene.Dir
		output, err = cmd.CombinedOutput()
		require.NoError(t, err, "split command failed: %s", string(output))

		// Verify new branch was created
		cmd = exec.Command("git", "branch")
		cmd.Dir = scene.Dir
		branchOutput, err := cmd.Output()
		require.NoError(t, err)
		branches := strings.Split(strings.TrimSpace(string(branchOutput)), "\n")
		branchNames := make([]string, len(branches))
		for i, b := range branches {
			branchNames[i] = strings.TrimSpace(strings.TrimPrefix(b, "* "))
		}
		require.Contains(t, branchNames, "feature_split", "should have created feature_split branch")

		// Checkout the new branch and verify it has file1 and file2
		err = scene.Repo.CheckoutBranch("feature_split")
		require.NoError(t, err)

		cmd = exec.Command("git", "ls-files", "file1_test.txt", "file2_test.txt")
		cmd.Dir = scene.Dir
		output, err = cmd.Output()
		require.NoError(t, err)
		require.Contains(t, string(output), "file1_test.txt")
		require.Contains(t, string(output), "file2_test.txt")
		require.NotContains(t, string(output), "file3_test.txt", "file3 should not be on feature_split")

		// Checkout original branch and verify it only has file3
		err = scene.Repo.CheckoutBranch("feature")
		require.NoError(t, err)

		cmd = exec.Command("git", "ls-files", "file3_test.txt")
		cmd.Dir = scene.Dir
		output, err = cmd.Output()
		require.NoError(t, err)
		require.Contains(t, string(output), "file3_test.txt", "file3 should remain on feature branch")

		// Verify file1 and file2 are not on feature branch
		cmd = exec.Command("git", "ls-files", "file1_test.txt", "file2_test.txt")
		cmd.Dir = scene.Dir
		output, err = cmd.Output()
		require.NoError(t, err)
		outputStr := string(output)
		require.NotContains(t, outputStr, "file1_test.txt", "file1 should not be on feature branch")
		require.NotContains(t, outputStr, "file2_test.txt", "file2 should not be on feature branch")
	})

	t.Run("split --by-file with single file", func(t *testing.T) {
		t.Parallel()
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

			// Create a feature branch
			if err := s.Repo.CreateChange("feature change 1", "test1", false); err != nil {
				return err
			}
			cmd = exec.Command(binaryPath, "create", "feature", "-m", "feature change 1")
			cmd.Dir = s.Dir
			if err := cmd.Run(); err != nil {
				return err
			}

			// Add a single file
			if err := s.Repo.CreateChange("file1 content", "file1", false); err != nil {
				return err
			}
			return s.Repo.CreateChangeAndCommit("Add file1", "Add file1")
		})

		// Run split --by-file with single file
		cmd := exec.Command(binaryPath, "split", "--by-file", "file1_test.txt", "--no-interactive")
		cmd.Dir = scene.Dir
		output, err := cmd.CombinedOutput()
		require.NoError(t, err, "split command failed: %s", string(output))

		// Verify new branch was created
		cmd = exec.Command("git", "branch")
		cmd.Dir = scene.Dir
		branchOutput, err := cmd.Output()
		require.NoError(t, err)
		require.Contains(t, string(branchOutput), "feature_split")

		// Verify file is on the new branch
		err = scene.Repo.CheckoutBranch("feature_split")
		require.NoError(t, err)

		cmd = exec.Command("git", "ls-files", "file1_test.txt")
		cmd.Dir = scene.Dir
		output, err = cmd.Output()
		require.NoError(t, err)
		require.Contains(t, string(output), "file1_test.txt")
	})

	t.Run("split --by-file restacks upstack branches", func(t *testing.T) {
		t.Parallel()
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

			// Create branch1
			if err := s.Repo.CreateChange("change 1", "test1", false); err != nil {
				return err
			}
			cmd = exec.Command(binaryPath, "create", "branch1", "-m", "change 1")
			cmd.Dir = s.Dir
			if err := cmd.Run(); err != nil {
				return err
			}

			// Add file to branch1
			if err := s.Repo.CreateChange("file1 content", "file1", false); err != nil {
				return err
			}
			if err := s.Repo.CreateChangeAndCommit("Add file1", "Add file1"); err != nil {
				return err
			}

			// Create branch2 on top of branch1
			if err := s.Repo.CreateChange("change 2", "test2", false); err != nil {
				return err
			}
			cmd = exec.Command(binaryPath, "create", "branch2", "-m", "change 2")
			cmd.Dir = s.Dir
			if err := cmd.Run(); err != nil {
				return err
			}

			return nil
		})

		// Verify we're on branch2
		currentBranch, err := scene.Repo.CurrentBranchName()
		require.NoError(t, err)
		require.Equal(t, "branch2", currentBranch)

		// Go back to branch1 to split it
		err = scene.Repo.CheckoutBranch("branch1")
		require.NoError(t, err)

		// Run split --by-file on branch1
		cmd := exec.Command(binaryPath, "split", "--by-file", "file1_test.txt", "--no-interactive")
		cmd.Dir = scene.Dir
		output, err := cmd.CombinedOutput()
		require.NoError(t, err, "split command failed: %s", string(output))

		// Verify branch2 was restacked (check that it still has its changes)
		err = scene.Repo.CheckoutBranch("branch2")
		require.NoError(t, err)

		// Verify branch2 still exists and has its changes
		cmd = exec.Command("git", "ls-files", "test2_test.txt")
		cmd.Dir = scene.Dir
		output, err = cmd.Output()
		require.NoError(t, err)
		require.Contains(t, string(output), "test2_test.txt", "branch2 should still have its changes after restack")
	})

	t.Run("split --by-file fails when not on a branch", func(t *testing.T) {
		t.Parallel()
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

			// Detach HEAD
			cmd = exec.Command("git", "checkout", "HEAD~0")
			cmd.Dir = s.Dir
			return cmd.Run()
		})

		// Try to run split in detached HEAD state
		cmd := exec.Command(binaryPath, "split", "--by-file", "somefile.txt", "--no-interactive")
		cmd.Dir = scene.Dir
		output, err := cmd.CombinedOutput()
		require.Error(t, err, "split should fail when not on a branch")
		require.Contains(t, string(output), "not on a branch", "error message should mention not on a branch")
	})

	t.Run("split --by-file fails with uncommitted changes", func(t *testing.T) {
		t.Parallel()
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

			// Create a feature branch with a file
			if err := s.Repo.CreateChange("feature change 1", "test1", false); err != nil {
				return err
			}
			cmd = exec.Command(binaryPath, "create", "feature", "-m", "feature change 1")
			cmd.Dir = s.Dir
			if err := cmd.Run(); err != nil {
				return err
			}

			// Modify the tracked file to create uncommitted changes
			// (test1_test.txt is tracked from the previous commit)
			return s.Repo.CreateChange("modified content", "test1", true)
		})

		// Try to run split with uncommitted changes
		cmd := exec.Command(binaryPath, "split", "--by-file", "test1_test.txt", "--no-interactive")
		cmd.Dir = scene.Dir
		output, err := cmd.CombinedOutput()
		require.Error(t, err, "split should fail with uncommitted changes")
		require.Contains(t, string(output), "uncommitted tracked changes", "error message should mention uncommitted changes")
	})

	t.Run("split --by-file with non-existent file fails gracefully", func(t *testing.T) {
		t.Parallel()
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

			// Create a feature branch
			if err := s.Repo.CreateChange("feature change 1", "test1", false); err != nil {
				return err
			}
			cmd = exec.Command(binaryPath, "create", "feature", "-m", "feature change 1")
			cmd.Dir = s.Dir
			return cmd.Run()
		})

		// Try to split with non-existent file
		cmd := exec.Command(binaryPath, "split", "--by-file", "nonexistent.txt", "--no-interactive")
		cmd.Dir = scene.Dir
		output, err := cmd.CombinedOutput()
		// Split should fail if file doesn't exist
		require.Error(t, err, "split should fail with non-existent file")
		require.Contains(t, string(output), "no changes found", "should show error for non-existent file")
	})
}
