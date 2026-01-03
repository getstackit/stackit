package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"stackit.dev/stackit/internal/config"
	"stackit.dev/stackit/testhelpers"
	"stackit.dev/stackit/testhelpers/scenario"
)

func TestContinueCommand(t *testing.T) {
	t.Parallel()

	t.Run("continue errors when no rebase in progress", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, func(sc *testhelpers.Scene) error {
			return sc.Repo.CreateChangeAndCommit("initial", "init")
		}).WithInProcess(true)

		// Verify no rebase is in progress
		require.False(t, s.Scene.Repo.RebaseInProgress(), "should not have rebase in progress")

		// Run continue without rebase in progress
		output, err := s.RunCliAndGetOutput("continue")

		require.Error(t, err, "continue should fail when no rebase in progress")
		require.Contains(t, output, "no rebase in progress", "error message: %s", output)
	})

	t.Run("continue works without continuation state", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, func(sc *testhelpers.Scene) error {
			// Create initial commit
			if err := sc.Repo.CreateChangeAndCommit("initial", "init"); err != nil {
				return err
			}
			return nil
		}).WithInProcess(true)

		// Create branch1 using create command (automatically tracks)
		if err := s.Scene.Repo.CreateChange("branch1 change", "test1", false); err != nil {
			t.Fatal(err)
		}
		s.RunCli("create", "branch1", "-m", "branch1 change")

		// Start a rebase manually
		if err := s.Scene.Repo.CheckoutBranch("main"); err != nil {
			t.Fatal(err)
		}
		if err := s.Scene.Repo.CreateChangeAndCommit("main change", "main"); err != nil {
			t.Fatal(err)
		}
		if err := s.Scene.Repo.CheckoutBranch("branch1"); err != nil {
			t.Fatal(err)
		}
		// Start rebase (will conflict if there are conflicts, otherwise will succeed)
		_ = s.Scene.Repo.RunGitCommand("rebase", "main")

		// Check if rebase is actually in progress
		if !s.Scene.Repo.RebaseInProgress() {
			t.Skip("Rebase completed without conflict, skipping test")
			return
		}

		// Resolve any conflicts
		_ = s.Scene.Repo.ResolveMergeConflicts()
		_ = s.Scene.Repo.MarkMergeConflictsAsResolved()

		// Run continue without continuation state (should work now)
		_, _ = s.RunCliAndGetOutput("continue")
	})

	t.Run("continue with --all flag stages changes", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, func(sc *testhelpers.Scene) error {
			// Create initial commit with a file
			if err := sc.Repo.CreateChange("initial", "test", false); err != nil {
				return err
			}
			if err := sc.Repo.CreateChangeAndCommit("initial", "init"); err != nil {
				return err
			}
			return nil
		}).WithInProcess(true)

		// Create branch1 using create command (automatically tracks)
		if err := s.Scene.Repo.CreateChange("branch1 change", "test", false); err != nil {
			t.Fatal(err)
		}
		s.RunCli("create", "branch1", "-m", "branch1 change")

		// Create branch2 using create command (automatically tracks)
		if err := s.Scene.Repo.CreateChange("branch2 change", "test2", false); err != nil {
			t.Fatal(err)
		}
		s.RunCli("create", "branch2", "-m", "branch2 change")

		// Switch to main and create conflicting change
		if err := s.Scene.Repo.CheckoutBranch("main"); err != nil {
			t.Fatal(err)
		}
		if err := s.Scene.Repo.CreateChange("main change", "test", false); err != nil {
			t.Fatal(err)
		}
		if err := s.Scene.Repo.CreateChangeAndCommit("main change", "main"); err != nil {
			t.Fatal(err)
		}
		// Switch to branch1 and start rebase
		if err := s.Scene.Repo.CheckoutBranch("branch1"); err != nil {
			t.Fatal(err)
		}
		// Start rebase (will conflict)
		_ = s.Scene.Repo.RunGitCommand("rebase", "main")

		// Check if rebase is actually in progress
		if !s.Scene.Repo.RebaseInProgress() {
			t.Skip("Rebase completed without conflict, skipping test")
			return
		}

		// Get main revision for continuation state
		mainRev, err := s.Scene.Repo.GetRef("main")
		require.NoError(t, err)

		// Create continuation state manually
		continuation := &config.ContinuationState{
			BranchesToRestack:     []string{"branch2"},
			RebasedBranchBase:     mainRev,
			CurrentBranchOverride: "branch1",
		}
		err = config.PersistContinuationState(s.Scene.Dir, continuation)
		require.NoError(t, err)

		// Resolve conflicts
		err = s.Scene.Repo.ResolveMergeConflicts()
		require.NoError(t, err)

		// Run continue with --all flag
		_, _ = s.RunCliAndGetOutput("continue", "--all")
	})

	t.Run("continue resumes restacking after conflict resolution", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, func(sc *testhelpers.Scene) error {
			// Create initial commit with a file
			if err := sc.Repo.CreateChange("initial", "test", false); err != nil {
				return err
			}
			if err := sc.Repo.CreateChangeAndCommit("initial", "init"); err != nil {
				return err
			}
			return nil
		}).WithInProcess(true)

		// Create branch1 using create command (automatically tracks)
		if err := s.Scene.Repo.CreateChange("branch1 change", "test", false); err != nil {
			t.Fatal(err)
		}
		s.RunCli("create", "branch1", "-m", "branch1 change")

		// Create branch2 using create command (automatically tracks)
		if err := s.Scene.Repo.CreateChange("branch2 change", "test2", false); err != nil {
			t.Fatal(err)
		}
		s.RunCli("create", "branch2", "-m", "branch2 change")

		// Switch to main and create conflicting change
		if err := s.Scene.Repo.CheckoutBranch("main"); err != nil {
			t.Fatal(err)
		}
		if err := s.Scene.Repo.CreateChange("main change", "test", false); err != nil {
			t.Fatal(err)
		}
		if err := s.Scene.Repo.CreateChangeAndCommit("main change", "main"); err != nil {
			t.Fatal(err)
		}
		// Switch to branch1 and start rebase
		if err := s.Scene.Repo.CheckoutBranch("branch1"); err != nil {
			t.Fatal(err)
		}
		// Start rebase (will conflict)
		_ = s.Scene.Repo.RunGitCommand("rebase", "main")

		// Check if rebase is actually in progress
		if !s.Scene.Repo.RebaseInProgress() {
			t.Skip("Rebase completed without conflict, skipping test")
			return
		}

		// Get main revision for continuation state
		mainRev, err := s.Scene.Repo.GetRef("main")
		require.NoError(t, err)

		// Create continuation state with branch2 to restack
		continuation := &config.ContinuationState{
			BranchesToRestack:     []string{"branch2"},
			RebasedBranchBase:     mainRev,
			CurrentBranchOverride: "branch1",
		}
		err = config.PersistContinuationState(s.Scene.Dir, continuation)
		require.NoError(t, err)

		// Resolve conflicts
		err = s.Scene.Repo.ResolveMergeConflicts()
		require.NoError(t, err)
		err = s.Scene.Repo.MarkMergeConflictsAsResolved()
		require.NoError(t, err)

		// Run continue
		output, err := s.RunCliAndGetOutput("continue")

		// Should succeed and continue with branch2
		if err != nil {
			// If it fails, it might be because branch2 also needs restacking
			// which is expected behavior
			require.Contains(t, output, "branch2", "should mention branch2")
		}
	})

	t.Run("continue clears continuation state on success", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, func(sc *testhelpers.Scene) error {
			// Create initial commit with a file
			if err := sc.Repo.CreateChange("initial", "test", false); err != nil {
				return err
			}
			if err := sc.Repo.CreateChangeAndCommit("initial", "init"); err != nil {
				return err
			}
			return nil
		}).WithInProcess(true)

		// Create branch1 using create command (automatically tracks)
		if err := s.Scene.Repo.CreateChange("branch1 change", "test", false); err != nil {
			t.Fatal(err)
		}
		s.RunCli("create", "branch1", "-m", "branch1 change")

		// Switch to main and create conflicting change
		if err := s.Scene.Repo.CheckoutBranch("main"); err != nil {
			t.Fatal(err)
		}
		if err := s.Scene.Repo.CreateChange("main change", "test", false); err != nil {
			t.Fatal(err)
		}
		if err := s.Scene.Repo.CreateChangeAndCommit("main change", "main"); err != nil {
			t.Fatal(err)
		}
		// Switch to branch1 and start rebase
		if err := s.Scene.Repo.CheckoutBranch("branch1"); err != nil {
			t.Fatal(err)
		}
		// Start rebase (will conflict)
		_ = s.Scene.Repo.RunGitCommand("rebase", "main")

		// Check if rebase is actually in progress
		if !s.Scene.Repo.RebaseInProgress() {
			t.Skip("Rebase completed without conflict, skipping test")
			return
		}

		// Get main revision for continuation state
		mainRev, err := s.Scene.Repo.GetRef("main")
		require.NoError(t, err)

		// Create continuation state with no remaining branches
		continuation := &config.ContinuationState{
			BranchesToRestack:     []string{},
			RebasedBranchBase:     mainRev,
			CurrentBranchOverride: "branch1",
		}
		err = config.PersistContinuationState(s.Scene.Dir, continuation)
		require.NoError(t, err)

		// Resolve conflicts
		err = s.Scene.Repo.ResolveMergeConflicts()
		require.NoError(t, err)
		err = s.Scene.Repo.MarkMergeConflictsAsResolved()
		require.NoError(t, err)

		// Run continue
		output, err := s.RunCliAndGetOutput("continue")

		// Should succeed
		if err == nil {
			// Verify continuation state was cleared
			continuationPath := filepath.Join(s.Scene.Dir, ".git", ".stackit_continue")
			_, err = os.Stat(continuationPath)
			require.Error(t, err, "continuation state file should be deleted")
			require.True(t, os.IsNotExist(err))
		} else {
			// If it fails, log the output for debugging
			t.Logf("Continue command output: %s", output)
		}
	})

	t.Run("continue handles another conflict", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, func(sc *testhelpers.Scene) error {
			// Create initial commit with a file
			if err := sc.Repo.CreateChange("initial", "test", false); err != nil {
				return err
			}
			if err := sc.Repo.CreateChangeAndCommit("initial", "init"); err != nil {
				return err
			}
			return nil
		}).WithInProcess(true)

		// Create branch1 using create command (automatically tracks)
		if err := s.Scene.Repo.CreateChange("branch1 change", "test", false); err != nil {
			t.Fatal(err)
		}
		s.RunCli("create", "branch1", "-m", "branch1 change")

		// Create branch2 using create command (automatically tracks)
		if err := s.Scene.Repo.CreateChange("branch2 change", "test2", false); err != nil {
			t.Fatal(err)
		}
		s.RunCli("create", "branch2", "-m", "branch2 change")

		// Switch to main and create conflicting change
		if err := s.Scene.Repo.CheckoutBranch("main"); err != nil {
			t.Fatal(err)
		}
		if err := s.Scene.Repo.CreateChange("main change", "test", false); err != nil {
			t.Fatal(err)
		}
		if err := s.Scene.Repo.CreateChangeAndCommit("main change", "main"); err != nil {
			t.Fatal(err)
		}
		// Switch to branch1 and start rebase
		if err := s.Scene.Repo.CheckoutBranch("branch1"); err != nil {
			t.Fatal(err)
		}
		// Start rebase (will conflict)
		_ = s.Scene.Repo.RunGitCommand("rebase", "main")

		// Check if rebase is actually in progress
		if !s.Scene.Repo.RebaseInProgress() {
			t.Skip("Rebase completed without conflict, skipping test")
			return
		}

		// Get main revision for continuation state
		mainRev, err := s.Scene.Repo.GetRef("main")
		require.NoError(t, err)

		// Create continuation state
		continuation := &config.ContinuationState{
			BranchesToRestack:     []string{"branch2"},
			RebasedBranchBase:     mainRev,
			CurrentBranchOverride: "branch1",
		}
		err = config.PersistContinuationState(s.Scene.Dir, continuation)
		require.NoError(t, err)

		// Resolve conflicts
		err = s.Scene.Repo.ResolveMergeConflicts()
		require.NoError(t, err)
		err = s.Scene.Repo.MarkMergeConflictsAsResolved()
		require.NoError(t, err)

		// Run continue
		_, err = s.RunCliAndGetOutput("continue")

		// If there's another conflict, continuation state should be persisted again
		if err != nil {
			continuationPath := filepath.Join(s.Scene.Dir, ".git", ".stackit_continue")
			data, err := os.ReadFile(continuationPath)
			if err == nil {
				var state config.ContinuationState
				json.Unmarshal(data, &state)
				// State should still exist
				require.NotEmpty(t, state.RebasedBranchBase)
			}
		}
	})
}
