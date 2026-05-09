package branch_test

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"stackit.dev/stackit/testhelpers"
)

func TestSquashCommand(t *testing.T) {
	t.Parallel()
	binaryPath := testhelpers.GetSharedBinaryPath()
	if binaryPath == "" {
		if err := testhelpers.GetBinaryError(); err != nil {
			t.Fatalf("failed to build stackit binary: %v", err)
		}
		t.Fatal("stackit binary not built")
	}

	t.Run("squash branch with multiple commits", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, func(s *testhelpers.Scene) error {
			// Create initial commit
			if err := testhelpers.InitialCommitSceneSetup(s); err != nil {
				return err
			}
			// Create branch with first commit
			if err := s.Repo.CreateChange("feature change 1", "test1", false); err != nil {
				return err
			}
			cmd := exec.Command(binaryPath, "create", "feature", "-m", "feature change 1")
			cmd.Dir = s.Dir
			if err := cmd.Run(); err != nil {
				return err
			}
			// Add second commit to the branch
			if err := s.Repo.CreateChange("feature change 2", "test2", false); err != nil {
				return err
			}
			if err := s.Repo.CreateChangeAndCommit("feature change 2", "feature change 2"); err != nil {
				return err
			}
			// Add third commit
			if err := s.Repo.CreateChange("feature change 3", "test3", false); err != nil {
				return err
			}
			return s.Repo.CreateChangeAndCommit("feature change 3", "feature change 3")
		})

		// Verify we have multiple commits
		cmd := exec.Command("git", "log", "--oneline", "main..feature")
		cmd.Dir = scene.Dir
		output := testhelpers.Must(cmd.CombinedOutput())
		require.Contains(t, string(output), "feature change 1")
		require.Contains(t, string(output), "feature change 2")
		require.Contains(t, string(output), "feature change 3")

		// Get commit count before squash
		cmd = exec.Command("git", "log", "--oneline", "main..feature")
		cmd.Dir = scene.Dir
		beforeOutput, _ := cmd.CombinedOutput()
		beforeCount := countLines(string(beforeOutput))
		require.Greater(t, beforeCount, 1, "should have multiple commits before squash")

		// Run squash with --no-edit
		cmd = exec.Command(binaryPath, "squash", "--no-edit")
		cmd.Dir = scene.Dir
		output, err := cmd.CombinedOutput()

		require.NoError(t, err, "squash command failed: %s", string(output))
		require.Contains(t, string(output), "Squashed commits", "should mention squashing")

		// Verify commits are squashed (should only have one commit now)
		cmd = exec.Command("git", "log", "--oneline", "main..feature")
		cmd.Dir = scene.Dir
		output = testhelpers.Must(cmd.CombinedOutput())
		// Should only have one commit line (the squashed commit)
		lines := countLines(string(output))
		require.Equal(t, 1, lines, "should have only one commit after squash, got: %s", string(output))
	})

	t.Run("squash with --message flag", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, func(s *testhelpers.Scene) error {
			// Create initial commit
			if err := testhelpers.InitialCommitSceneSetup(s); err != nil {
				return err
			}
			// Create branch with first commit
			if err := s.Repo.CreateChange("feature change 1", "test1", false); err != nil {
				return err
			}
			cmd := exec.Command(binaryPath, "create", "feature", "-m", "feature change 1")
			cmd.Dir = s.Dir
			if err := cmd.Run(); err != nil {
				return err
			}
			// Add second commit
			if err := s.Repo.CreateChange("feature change 2", "test2", false); err != nil {
				return err
			}
			return s.Repo.CreateChangeAndCommit("feature change 2", "feature change 2")
		})

		// Run squash with --message
		cmd := exec.Command(binaryPath, "squash", "-m", "Squashed feature changes")
		cmd.Dir = scene.Dir
		output, err := cmd.CombinedOutput()

		require.NoError(t, err, "squash command failed: %s", string(output))

		// Verify the commit message is the new one
		cmd = exec.Command("git", "log", "-1", "--format=%s", "feature")
		cmd.Dir = scene.Dir
		output = testhelpers.Must(cmd.CombinedOutput())
		require.Contains(t, string(output), "Squashed feature changes")
	})

	t.Run("squash and restack child branch", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, func(s *testhelpers.Scene) error {
			// Create initial commit
			if err := testhelpers.InitialCommitSceneSetup(s); err != nil {
				return err
			}
			// Create branch A with two commits
			if err := s.Repo.CreateChange("a change 1", "a1", false); err != nil {
				return err
			}
			cmd := exec.Command(binaryPath, "create", "a", "-m", "a change 1")
			cmd.Dir = s.Dir
			if err := cmd.Run(); err != nil {
				return err
			}
			if err := s.Repo.CreateChange("a change 2", "a2", false); err != nil {
				return err
			}
			if err := s.Repo.CreateChangeAndCommit("a change 2", "a change 2"); err != nil {
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

		// Run squash on A
		cmd := exec.Command(binaryPath, "squash", "-n")
		cmd.Dir = scene.Dir
		output, err := cmd.CombinedOutput()

		require.NoError(t, err, "squash command failed: %s", string(output))
		require.Contains(t, string(output), "Squashed commits", "should mention squashing")

		// Switch to branch B and verify it was restacked
		require.NoError(t, scene.Repo.CheckoutBranch("b"))

		// Verify B is still based on A (restacked successfully)
		cmd = exec.Command("git", "log", "--oneline", "a..b")
		cmd.Dir = scene.Dir
		output = testhelpers.Must(cmd.CombinedOutput())
		require.Contains(t, string(output), "b change", "branch B should still have its commit")
	})

	t.Run("squash errors on trunk branch", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)

		// Try to squash trunk (main)
		cmd := exec.Command(binaryPath, "squash", "-n")
		cmd.Dir = scene.Dir
		output, err := cmd.CombinedOutput()

		require.Error(t, err, "squash should fail on trunk")
		require.Contains(t, string(output), "cannot squash trunk", "should mention trunk error")
	})

	t.Run("squash errors when not on a branch", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)

		// Detach HEAD
		require.NoError(t, scene.Repo.RunGitCommand("checkout", "HEAD~0"))

		// Try to squash
		cmd := exec.Command(binaryPath, "squash", "-n")
		cmd.Dir = scene.Dir
		output, err := cmd.CombinedOutput()

		require.Error(t, err, "squash should fail when not on a branch")
		require.Contains(t, string(output), "not on a branch", "should mention not on branch error")
	})

	t.Run("squash errors when stackit not initialized", func(t *testing.T) {
		t.Parallel()
		// Create a temporary directory without stackit initialization
		tmpDir := t.TempDir()
		cmd := exec.Command("git", "init", tmpDir, "-b", "main")
		require.NoError(t, cmd.Run())

		// Create a commit
		cmd = exec.Command("git", "-C", tmpDir, "commit", "--allow-empty", "-m", "initial")
		require.NoError(t, cmd.Run())

		// Try to squash without initializing stackit
		cmd = exec.Command(binaryPath, "squash", "-n")
		cmd.Dir = tmpDir
		output, err := cmd.CombinedOutput()

		require.Error(t, err, "squash should fail when stackit not initialized")
		require.Contains(t, string(output), "not initialized", "should mention not initialized")
	})

	t.Run("squash with single commit", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, func(s *testhelpers.Scene) error {
			// Create initial commit
			if err := testhelpers.InitialCommitSceneSetup(s); err != nil {
				return err
			}
			// Create branch with single commit
			if err := s.Repo.CreateChange("feature change", "test", false); err != nil {
				return err
			}
			cmd := exec.Command(binaryPath, "create", "feature", "-m", "feature change")
			cmd.Dir = s.Dir
			return cmd.Run()
		})

		// Run squash
		cmd := exec.Command(binaryPath, "squash", "-n")
		cmd.Dir = scene.Dir
		output, err := cmd.CombinedOutput()

		require.NoError(t, err, "squash should work with single commit: %s", string(output))

		// Verify still has one commit
		cmd = exec.Command("git", "log", "--oneline", "main..feature")
		cmd.Dir = scene.Dir
		output = testhelpers.Must(cmd.CombinedOutput())
		lines := countLines(string(output))
		require.Equal(t, 1, lines, "should still have one commit after squash")
	})

	t.Run("squash single-commit child does not affect parent", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, func(s *testhelpers.Scene) error {
			// Create initial commit
			if err := testhelpers.InitialCommitSceneSetup(s); err != nil {
				return err
			}
			// Create branch A with two commits
			if err := s.Repo.CreateChange("a change 1", "a1", false); err != nil {
				return err
			}
			cmd := exec.Command(binaryPath, "create", "a", "-m", "a change 1")
			cmd.Dir = s.Dir
			if err := cmd.Run(); err != nil {
				return err
			}
			if err := s.Repo.CreateChange("a change 2", "a2", false); err != nil {
				return err
			}
			if err := s.Repo.CreateChangeAndCommit("a change 2", "a change 2"); err != nil {
				return err
			}
			// Create branch B on top of A with single commit
			if err := s.Repo.CreateChange("b change", "b", false); err != nil {
				return err
			}
			cmd = exec.Command(binaryPath, "create", "b", "-m", "b change")
			cmd.Dir = s.Dir
			return cmd.Run()
		})

		// Record A's commit SHA before squashing B
		cmd := exec.Command("git", "rev-parse", "a")
		cmd.Dir = scene.Dir
		aRevBefore := strings.TrimSpace(string(testhelpers.Must(cmd.CombinedOutput())))

		// Record A's commit count before squashing B
		cmd = exec.Command("git", "log", "--oneline", "main..a")
		cmd.Dir = scene.Dir
		aLogBefore := string(testhelpers.Must(cmd.CombinedOutput()))
		aCommitCountBefore := countLines(aLogBefore)
		require.Equal(t, 2, aCommitCountBefore, "A should have 2 commits before squash")

		// Run squash on B (the child with single commit)
		cmd = exec.Command(binaryPath, "squash", "-n")
		cmd.Dir = scene.Dir
		output, err := cmd.CombinedOutput()
		require.NoError(t, err, "squash should work on single commit branch: %s", string(output))

		// Verify A's commit SHA is unchanged
		cmd = exec.Command("git", "rev-parse", "a")
		cmd.Dir = scene.Dir
		aRevAfter := strings.TrimSpace(string(testhelpers.Must(cmd.CombinedOutput())))
		require.Equal(t, aRevBefore, aRevAfter, "parent branch A's SHA should not change when squashing child B")

		// Verify A still has 2 commits
		cmd = exec.Command("git", "log", "--oneline", "main..a")
		cmd.Dir = scene.Dir
		aLogAfter := string(testhelpers.Must(cmd.CombinedOutput()))
		aCommitCountAfter := countLines(aLogAfter)
		require.Equal(t, 2, aCommitCountAfter, "A should still have 2 commits after squashing B")

		// Verify A's commits are the same
		require.Equal(t, aLogBefore, aLogAfter, "A's commit history should be unchanged")

		// Verify B still has 1 commit
		cmd = exec.Command("git", "log", "--oneline", "a..b")
		cmd.Dir = scene.Dir
		output = testhelpers.Must(cmd.CombinedOutput())
		bCommitCount := countLines(string(output))
		require.Equal(t, 1, bCommitCount, "B should still have 1 commit after squash")
	})

	t.Run("squash restacks multiple upstack branches", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, func(s *testhelpers.Scene) error {
			// Create initial commit
			if err := testhelpers.InitialCommitSceneSetup(s); err != nil {
				return err
			}
			// Create branch A with two commits
			if err := s.Repo.CreateChange("a change 1", "a1", false); err != nil {
				return err
			}
			cmd := exec.Command(binaryPath, "create", "a", "-m", "a change 1")
			cmd.Dir = s.Dir
			if err := cmd.Run(); err != nil {
				return err
			}
			if err := s.Repo.CreateChange("a change 2", "a2", false); err != nil {
				return err
			}
			if err := s.Repo.CreateChangeAndCommit("a change 2", "a change 2"); err != nil {
				return err
			}
			// Create branch B on top of A
			if err := s.Repo.CreateChange("b change", "b", false); err != nil {
				return err
			}
			cmd = exec.Command(binaryPath, "create", "b", "-m", "b change")
			cmd.Dir = s.Dir
			if err := cmd.Run(); err != nil {
				return err
			}
			// Create branch C on top of B
			if err := s.Repo.CreateChange("c change", "c", false); err != nil {
				return err
			}
			cmd = exec.Command(binaryPath, "create", "c", "-m", "c change")
			cmd.Dir = s.Dir
			return cmd.Run()
		})

		// Switch to branch A
		require.NoError(t, scene.Repo.CheckoutBranch("a"))

		// Run squash on A
		cmd := exec.Command(binaryPath, "squash", "-n")
		cmd.Dir = scene.Dir
		output, err := cmd.CombinedOutput()

		require.NoError(t, err, "squash command failed: %s", string(output))

		// Verify B and C were restacked (they should still be valid)
		require.NoError(t, scene.Repo.CheckoutBranch("b"))
		cmd = exec.Command("git", "log", "--oneline", "a..b")
		cmd.Dir = scene.Dir
		output = testhelpers.Must(cmd.CombinedOutput())
		require.Contains(t, string(output), "b change")

		require.NoError(t, scene.Repo.CheckoutBranch("c"))
		cmd = exec.Command("git", "log", "--oneline", "b..c")
		cmd.Dir = scene.Dir
		output = testhelpers.Must(cmd.CombinedOutput())
		require.Contains(t, string(output), "c change")
	})
}

// countLines counts the number of non-empty lines in a string
func countLines(s string) int {
	if s == "" {
		return 0
	}
	lines := strings.Split(strings.TrimSpace(s), "\n")
	count := 0
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			count++
		}
	}
	return count
}
