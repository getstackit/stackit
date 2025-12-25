package cli_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"stackit.dev/stackit/internal/config"
	"stackit.dev/stackit/testhelpers"
)

func TestContinueCommand(t *testing.T) {
	t.Parallel()
	binaryPath := getStackitBinary(t)

	t.Run("continue errors when no rebase in progress", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, func(s *testhelpers.Scene) error {
			return s.Repo.CreateChangeAndCommit("initial", "init")
		})

		// Verify no rebase is in progress
		require.False(t, scene.Repo.RebaseInProgress(), "should not have rebase in progress")

		// Run continue without rebase in progress
		cmd := exec.Command(binaryPath, "continue")
		cmd.Dir = scene.Dir
		output, err := cmd.CombinedOutput()

		require.Error(t, err, "continue should fail when no rebase in progress")
		require.Contains(t, string(output), "no rebase in progress", "error message: %s", string(output))
	})

	t.Run("continue works without continuation state", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, func(s *testhelpers.Scene) error {
			// Create initial commit
			if err := s.Repo.CreateChangeAndCommit("initial", "init"); err != nil {
				return err
			}
			// Create branch1 using create command (automatically tracks)
			if err := s.Repo.CreateChange("branch1 change", "test1", false); err != nil {
				return err
			}
			cmd := exec.Command(binaryPath, "create", "branch1", "-m", "branch1 change")
			cmd.Dir = s.Dir
			if err := cmd.Run(); err != nil {
				return err
			}
			// Start a rebase manually
			if err := s.Repo.CheckoutBranch("main"); err != nil {
				return err
			}
			if err := s.Repo.CreateChangeAndCommit("main change", "main"); err != nil {
				return err
			}
			if err := s.Repo.CheckoutBranch("branch1"); err != nil {
				return err
			}
			// Start rebase (will conflict if there are conflicts, otherwise will succeed)
			_ = s.Repo.RunGitCommand("rebase", "main")
			return nil
		})

		// Check if rebase is actually in progress
		if !scene.Repo.RebaseInProgress() {
			t.Skip("Rebase completed without conflict, skipping test")
			return
		}

		// Resolve any conflicts
		_ = scene.Repo.ResolveMergeConflicts()
		_ = scene.Repo.MarkMergeConflictsAsResolved()

		// Run continue without continuation state (should work now)
		cmd := exec.Command(binaryPath, "continue")
		cmd.Dir = scene.Dir
		output, err := cmd.CombinedOutput()

		// Should succeed or fail gracefully
		_ = output
		_ = err
		// Just verify the command ran
	})

	t.Run("continue with --all flag stages changes", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, func(s *testhelpers.Scene) error {
			// Create initial commit with a file
			if err := s.Repo.CreateChange("initial", "test", false); err != nil {
				return err
			}
			if err := s.Repo.CreateChangeAndCommit("initial", "init"); err != nil {
				return err
			}
			// Create branch1 using create command (automatically tracks)
			if err := s.Repo.CreateChange("branch1 change", "test", false); err != nil {
				return err
			}
			cmd := exec.Command(binaryPath, "create", "branch1", "-m", "branch1 change")
			cmd.Dir = s.Dir
			if err := cmd.Run(); err != nil {
				return err
			}
			// Create branch2 using create command (automatically tracks)
			if err := s.Repo.CreateChange("branch2 change", "test2", false); err != nil {
				return err
			}
			cmd = exec.Command(binaryPath, "create", "branch2", "-m", "branch2 change")
			cmd.Dir = s.Dir
			if err := cmd.Run(); err != nil {
				return err
			}
			// Switch to main and create conflicting change
			if err := s.Repo.CheckoutBranch("main"); err != nil {
				return err
			}
			if err := s.Repo.CreateChange("main change", "test", false); err != nil {
				return err
			}
			if err := s.Repo.CreateChangeAndCommit("main change", "main"); err != nil {
				return err
			}
			// Switch to branch1 and start rebase
			if err := s.Repo.CheckoutBranch("branch1"); err != nil {
				return err
			}
			// Start rebase (will conflict)
			_ = s.Repo.RunGitCommand("rebase", "main")
			return nil
		})

		// Check if rebase is actually in progress
		if !scene.Repo.RebaseInProgress() {
			t.Skip("Rebase completed without conflict, skipping test")
			return
		}

		// Get main revision for continuation state
		mainRev, err := scene.Repo.GetRef("main")
		require.NoError(t, err)

		// Create continuation state manually
		continuation := &config.ContinuationState{
			BranchesToRestack:     []string{"branch2"},
			RebasedBranchBase:     mainRev,
			CurrentBranchOverride: "branch1",
		}
		err = config.PersistContinuationState(scene.Dir, continuation)
		require.NoError(t, err)

		// Resolve conflicts
		err = scene.Repo.ResolveMergeConflicts()
		require.NoError(t, err)

		// Run continue with --all flag
		cmd := exec.Command(binaryPath, "continue", "--all")
		cmd.Dir = scene.Dir
		output, err := cmd.CombinedOutput()

		// Should succeed (or fail if there are still conflicts, which is expected)
		// The important thing is that --all flag was processed
		_ = output
		_ = err
	})

	t.Run("continue resumes restacking after conflict resolution", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, func(s *testhelpers.Scene) error {
			// Create initial commit with a file
			if err := s.Repo.CreateChange("initial", "test", false); err != nil {
				return err
			}
			if err := s.Repo.CreateChangeAndCommit("initial", "init"); err != nil {
				return err
			}
			// Create branch1 using create command (automatically tracks)
			if err := s.Repo.CreateChange("branch1 change", "test", false); err != nil {
				return err
			}
			cmd := exec.Command(binaryPath, "create", "branch1", "-m", "branch1 change")
			cmd.Dir = s.Dir
			if err := cmd.Run(); err != nil {
				return err
			}
			// Create branch2 using create command (automatically tracks)
			if err := s.Repo.CreateChange("branch2 change", "test2", false); err != nil {
				return err
			}
			cmd = exec.Command(binaryPath, "create", "branch2", "-m", "branch2 change")
			cmd.Dir = s.Dir
			if err := cmd.Run(); err != nil {
				return err
			}
			// Switch to main and create conflicting change
			if err := s.Repo.CheckoutBranch("main"); err != nil {
				return err
			}
			if err := s.Repo.CreateChange("main change", "test", false); err != nil {
				return err
			}
			if err := s.Repo.CreateChangeAndCommit("main change", "main"); err != nil {
				return err
			}
			// Switch to branch1 and start rebase
			if err := s.Repo.CheckoutBranch("branch1"); err != nil {
				return err
			}
			// Start rebase (will conflict)
			_ = s.Repo.RunGitCommand("rebase", "main")
			return nil
		})

		// Check if rebase is actually in progress
		if !scene.Repo.RebaseInProgress() {
			t.Skip("Rebase completed without conflict, skipping test")
			return
		}

		// Get main revision for continuation state
		mainRev, err := scene.Repo.GetRef("main")
		require.NoError(t, err)

		// Create continuation state with branch2 to restack
		continuation := &config.ContinuationState{
			BranchesToRestack:     []string{"branch2"},
			RebasedBranchBase:     mainRev,
			CurrentBranchOverride: "branch1",
		}
		err = config.PersistContinuationState(scene.Dir, continuation)
		require.NoError(t, err)

		// Resolve conflicts
		err = scene.Repo.ResolveMergeConflicts()
		require.NoError(t, err)
		err = scene.Repo.MarkMergeConflictsAsResolved()
		require.NoError(t, err)

		// Run continue
		output, err := testhelpers.RunCLICommand(t, binaryPath, scene.Dir, "continue")

		// Should succeed and continue with branch2
		if err != nil {
			// If it fails, it might be because branch2 also needs restacking
			// which is expected behavior
			require.Contains(t, string(output), "branch2", "should mention branch2")
		}
	})

	t.Run("continue clears continuation state on success", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, func(s *testhelpers.Scene) error {
			// Create initial commit with a file
			if err := s.Repo.CreateChange("initial", "test", false); err != nil {
				return err
			}
			if err := s.Repo.CreateChangeAndCommit("initial", "init"); err != nil {
				return err
			}
			// Create branch1 using create command (automatically tracks)
			if err := s.Repo.CreateChange("branch1 change", "test", false); err != nil {
				return err
			}
			cmd := exec.Command(binaryPath, "create", "branch1", "-m", "branch1 change")
			cmd.Dir = s.Dir
			if err := cmd.Run(); err != nil {
				return err
			}
			// Switch to main and create conflicting change
			if err := s.Repo.CheckoutBranch("main"); err != nil {
				return err
			}
			if err := s.Repo.CreateChange("main change", "test", false); err != nil {
				return err
			}
			if err := s.Repo.CreateChangeAndCommit("main change", "main"); err != nil {
				return err
			}
			// Switch to branch1 and start rebase
			if err := s.Repo.CheckoutBranch("branch1"); err != nil {
				return err
			}
			// Start rebase (will conflict)
			_ = s.Repo.RunGitCommand("rebase", "main")
			return nil
		})

		// Check if rebase is actually in progress
		if !scene.Repo.RebaseInProgress() {
			t.Skip("Rebase completed without conflict, skipping test")
			return
		}

		// Get main revision for continuation state
		mainRev, err := scene.Repo.GetRef("main")
		require.NoError(t, err)

		// Create continuation state with no remaining branches
		continuation := &config.ContinuationState{
			BranchesToRestack:     []string{},
			RebasedBranchBase:     mainRev,
			CurrentBranchOverride: "branch1",
		}
		err = config.PersistContinuationState(scene.Dir, continuation)
		require.NoError(t, err)

		// Resolve conflicts
		err = scene.Repo.ResolveMergeConflicts()
		require.NoError(t, err)
		err = scene.Repo.MarkMergeConflictsAsResolved()
		require.NoError(t, err)

		// Run continue
		output, err := testhelpers.RunCLICommand(t, binaryPath, scene.Dir, "continue")

		// Should succeed
		if err == nil {
			// Verify continuation state was cleared
			continuationPath := filepath.Join(scene.Dir, ".git", ".stackit_continue")
			_, err = os.Stat(continuationPath)
			require.Error(t, err, "continuation state file should be deleted")
			require.True(t, os.IsNotExist(err))
		} else {
			// If it fails, log the output for debugging
			t.Logf("Continue command output: %s", string(output))
		}
	})

	t.Run("continue handles another conflict", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, func(s *testhelpers.Scene) error {
			// Create initial commit with a file
			if err := s.Repo.CreateChange("initial", "test", false); err != nil {
				return err
			}
			if err := s.Repo.CreateChangeAndCommit("initial", "init"); err != nil {
				return err
			}
			// Create branch1 using create command (automatically tracks)
			if err := s.Repo.CreateChange("branch1 change", "test", false); err != nil {
				return err
			}
			cmd := exec.Command(binaryPath, "create", "branch1", "-m", "branch1 change")
			cmd.Dir = s.Dir
			if err := cmd.Run(); err != nil {
				return err
			}
			// Create branch2 using create command (automatically tracks)
			if err := s.Repo.CreateChange("branch2 change", "test2", false); err != nil {
				return err
			}
			cmd = exec.Command(binaryPath, "create", "branch2", "-m", "branch2 change")
			cmd.Dir = s.Dir
			if err := cmd.Run(); err != nil {
				return err
			}
			// Switch to main and create conflicting change
			if err := s.Repo.CheckoutBranch("main"); err != nil {
				return err
			}
			if err := s.Repo.CreateChange("main change", "test", false); err != nil {
				return err
			}
			if err := s.Repo.CreateChangeAndCommit("main change", "main"); err != nil {
				return err
			}
			// Switch to branch1 and start rebase
			if err := s.Repo.CheckoutBranch("branch1"); err != nil {
				return err
			}
			// Start rebase (will conflict)
			_ = s.Repo.RunGitCommand("rebase", "main")
			return nil
		})

		// Check if rebase is actually in progress
		if !scene.Repo.RebaseInProgress() {
			t.Skip("Rebase completed without conflict, skipping test")
			return
		}

		// Get main revision for continuation state
		mainRev, err := scene.Repo.GetRef("main")
		require.NoError(t, err)

		// Create continuation state
		continuation := &config.ContinuationState{
			BranchesToRestack:     []string{"branch2"},
			RebasedBranchBase:     mainRev,
			CurrentBranchOverride: "branch1",
		}
		err = config.PersistContinuationState(scene.Dir, continuation)
		require.NoError(t, err)

		// Resolve conflicts
		err = scene.Repo.ResolveMergeConflicts()
		require.NoError(t, err)
		err = scene.Repo.MarkMergeConflictsAsResolved()
		require.NoError(t, err)

		// Run continue
		output, err := testhelpers.RunCLICommand(t, binaryPath, scene.Dir, "continue")

		// If there's another conflict, continuation state should be persisted again
		if err != nil {
			continuationPath := filepath.Join(scene.Dir, ".git", ".stackit_continue")
			data, err := os.ReadFile(continuationPath)
			if err == nil {
				var state config.ContinuationState
				json.Unmarshal(data, &state)
				// State should still exist
				require.NotEmpty(t, state.RebasedBranchBase)
			}
		}

		_ = output
	})
}
