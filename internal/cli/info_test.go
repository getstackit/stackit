package cli_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"stackit.dev/stackit/testhelpers"
	"stackit.dev/stackit/testhelpers/scenario"
)

func TestInfoCommand(t *testing.T) {
	t.Parallel()

	t.Run("basic info display on current branch", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, func(sc *testhelpers.Scene) error {
			// Create initial commit
			if err := sc.Repo.CreateChangeAndCommit("initial", "init"); err != nil {
				return err
			}
			return nil
		}).WithInProcess(true)

		// Create branch with commit
		if err := s.Scene.Repo.CreateChange("feature change", "test", false); err != nil {
			t.Fatal(err)
		}
		s.RunCli("create", "feature", "-m", "feature change")

		// Run info command
		output, err := s.RunCliAndGetOutput("info")

		require.NoError(t, err, "info command failed: %s", output)
		require.Contains(t, output, "feature", "should contain branch name")
		require.Contains(t, output, "(current)", "should indicate current branch")
		require.Contains(t, output, "feature change", "should contain commit message")
	})

	t.Run("info for specified branch argument", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, func(sc *testhelpers.Scene) error {
			// Create initial commit
			if err := sc.Repo.CreateChangeAndCommit("initial", "init"); err != nil {
				return err
			}
			return nil
		}).WithInProcess(true)

		// Create branch A
		if err := s.Scene.Repo.CreateChange("a change", "a", false); err != nil {
			t.Fatal(err)
		}
		s.RunCli("create", "a", "-m", "a change")

		// Create branch B
		if err := s.Scene.Repo.CreateChange("b change", "b", false); err != nil {
			t.Fatal(err)
		}
		s.RunCli("create", "b", "-m", "b change")

		// Switch to branch b
		require.NoError(t, s.Scene.Repo.CheckoutBranch("b"))

		// Run info for branch a
		output, err := s.RunCliAndGetOutput("info", "a")

		require.NoError(t, err, "info command failed: %s", output)
		require.Contains(t, output, "a", "should contain branch name")
		require.Contains(t, output, "a change", "should contain commit message")
		require.NotContains(t, output, "(current)", "should not indicate current branch")
	})

	t.Run("info shows parent branch", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, func(sc *testhelpers.Scene) error {
			// Create initial commit
			if err := sc.Repo.CreateChangeAndCommit("initial", "init"); err != nil {
				return err
			}
			return nil
		}).WithInProcess(true)

		// Create branch A
		if err := s.Scene.Repo.CreateChange("a change", "a", false); err != nil {
			t.Fatal(err)
		}
		s.RunCli("create", "a", "-m", "a change")

		// Create branch B on top of A
		if err := s.Scene.Repo.CreateChange("b change", "b", false); err != nil {
			t.Fatal(err)
		}
		s.RunCli("create", "b", "-m", "b change")

		// Run info for branch b
		output, err := s.RunCliAndGetOutput("info", "b")

		require.NoError(t, err, "info command failed: %s", output)
		require.Contains(t, output, "Parent", "should show parent section")
		require.Contains(t, output, "a", "should show parent branch name")
	})

	t.Run("info shows children branches", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, func(sc *testhelpers.Scene) error {
			// Create initial commit
			if err := sc.Repo.CreateChangeAndCommit("initial", "init"); err != nil {
				return err
			}
			return nil
		}).WithInProcess(true)

		// Create branch A
		if err := s.Scene.Repo.CreateChange("a change", "a", false); err != nil {
			t.Fatal(err)
		}
		s.RunCli("create", "a", "-m", "a change")

		// Create branch B on top of A
		if err := s.Scene.Repo.CreateChange("b change", "b", false); err != nil {
			t.Fatal(err)
		}
		s.RunCli("create", "b", "-m", "b change")

		// Run info for branch a
		output, err := s.RunCliAndGetOutput("info", "a")

		require.NoError(t, err, "info command failed: %s", output)
		require.Contains(t, output, "Children", "should show children section")
		require.Contains(t, output, "b", "should show child branch name")
	})

	t.Run("info with --diff flag shows diff", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, func(sc *testhelpers.Scene) error {
			// Create initial commit
			if err := sc.Repo.CreateChangeAndCommit("initial", "init"); err != nil {
				return err
			}
			return nil
		}).WithInProcess(true)

		// Create branch with change
		if err := s.Scene.Repo.CreateChange("feature change", "test", false); err != nil {
			t.Fatal(err)
		}
		s.RunCli("create", "feature", "-m", "feature change")

		// Run info with --diff
		output, err := s.RunCliAndGetOutput("info", "--diff")

		require.NoError(t, err, "info command failed: %s", output)
		// Should contain diff markers
		require.True(t, strings.Contains(output, "+++") || strings.Contains(output, "---") || strings.Contains(output, "@@"),
			"should contain diff output, got: %s", output)
	})

	t.Run("info with --patch flag shows patches", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, func(sc *testhelpers.Scene) error {
			// Create initial commit
			if err := sc.Repo.CreateChangeAndCommit("initial", "init"); err != nil {
				return err
			}
			return nil
		}).WithInProcess(true)

		// Create branch with change
		if err := s.Scene.Repo.CreateChange("feature change", "test", false); err != nil {
			t.Fatal(err)
		}
		s.RunCli("create", "feature", "-m", "feature change")

		// Run info with --patch
		output, err := s.RunCliAndGetOutput("info", "--patch")

		require.NoError(t, err, "info command failed: %s", output)
		// Should contain patch markers
		require.True(t, strings.Contains(output, "+++") || strings.Contains(output, "---") || strings.Contains(output, "@@"),
			"should contain patch output, got: %s", output)
	})

	t.Run("info with --stat flag shows diffstat", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, func(sc *testhelpers.Scene) error {
			// Create initial commit
			if err := sc.Repo.CreateChangeAndCommit("initial", "init"); err != nil {
				return err
			}
			return nil
		}).WithInProcess(true)

		// Create branch with change
		if err := s.Scene.Repo.CreateChange("feature change", "test", false); err != nil {
			t.Fatal(err)
		}
		s.RunCli("create", "feature", "-m", "feature change")

		// Run info with --stat (implies --diff)
		output, err := s.RunCliAndGetOutput("info", "--stat")

		require.NoError(t, err, "info command failed: %s", output)
		// Should contain stat output (file names and change counts)
		require.True(t, strings.Contains(output, "test") || strings.Contains(output, "|"),
			"should contain diffstat output, got: %s", output)
	})

	t.Run("info with --stat --diff shows diffstat", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, func(sc *testhelpers.Scene) error {
			// Create initial commit
			if err := sc.Repo.CreateChangeAndCommit("initial", "init"); err != nil {
				return err
			}
			return nil
		}).WithInProcess(true)

		// Create branch with change
		if err := s.Scene.Repo.CreateChange("feature change", "test", false); err != nil {
			t.Fatal(err)
		}
		s.RunCli("create", "feature", "-m", "feature change")

		// Run info with --stat --diff
		output, err := s.RunCliAndGetOutput("info", "--stat", "--diff")

		require.NoError(t, err, "info command failed: %s", output)
		// Should contain stat output
		require.True(t, strings.Contains(output, "test") || strings.Contains(output, "|"),
			"should contain diffstat output, got: %s", output)
	})

	t.Run("info with --stat --patch shows stat per commit", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, func(sc *testhelpers.Scene) error {
			// Create initial commit
			if err := sc.Repo.CreateChangeAndCommit("initial", "init"); err != nil {
				return err
			}
			return nil
		}).WithInProcess(true)

		// Create branch with change
		if err := s.Scene.Repo.CreateChange("feature change", "test", false); err != nil {
			t.Fatal(err)
		}
		s.RunCli("create", "feature", "-m", "feature change")

		// Run info with --stat --patch
		output, err := s.RunCliAndGetOutput("info", "--stat", "--patch")

		require.NoError(t, err, "info command failed: %s", output)
		// Should contain stat output
		require.True(t, strings.Contains(output, "test") || strings.Contains(output, "|"),
			"should contain diffstat output, got: %s", output)
	})

	t.Run("info errors on non-existent branch", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, func(sc *testhelpers.Scene) error {
			return sc.Repo.CreateChangeAndCommit("initial", "init")
		}).WithInProcess(true)

		// Run info for non-existent branch
		output, err := s.RunCliAndGetOutput("info", "nonexistent")

		require.Error(t, err, "info should fail for non-existent branch")
		require.Contains(t, output, "does not exist", "should mention branch does not exist")
	})

	t.Run("info works on trunk", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, func(sc *testhelpers.Scene) error {
			return sc.Repo.CreateChangeAndCommit("initial", "init")
		}).WithInProcess(true)

		// Run info on trunk (main)
		output, err := s.RunCliAndGetOutput("info", "main")

		require.NoError(t, err, "info command failed: %s", output)
		require.Contains(t, output, "main", "should contain trunk branch name")
	})

	t.Run("info errors when not on branch and no branch specified", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, func(sc *testhelpers.Scene) error {
			return sc.Repo.CreateChangeAndCommit("initial", "init")
		}).WithInProcess(true)

		// Detach HEAD
		require.NoError(t, s.Scene.Repo.RunGitCommand("checkout", "HEAD~0"))

		// Run info without branch argument
		output, err := s.RunCliAndGetOutput("info")

		require.Error(t, err, "info should fail when not on branch")
		require.Contains(t, output, "not on a branch", "should mention not on branch")
	})

	t.Run("info_errors_when_stackit_not_initialized", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenarioParallel(t, nil).WithInProcess(true)

		// Create a temporary directory without stackit initialization
		tmpDir := t.TempDir()
		s.Scene.Dir = tmpDir

		// Initialize git
		require.NoError(t, s.Scene.Repo.RunGitCommand("init", tmpDir, "-b", "main"))

		// Create a commit
		require.NoError(t, s.Scene.Repo.RunGitCommand("-C", tmpDir, "commit", "--allow-empty", "-m", "initial"))

		// Try to run info without initializing stackit
		output, err := s.RunCliAndGetOutput("info")

		require.Error(t, err, "info should fail when stackit not initialized")
		require.Contains(t, output, "not initialized", "should mention not initialized")
	})

	t.Run("info with --stack --json shows JSON output", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, func(sc *testhelpers.Scene) error {
			// Create initial commit
			if err := sc.Repo.CreateChangeAndCommit("initial", "init"); err != nil {
				return err
			}
			return nil
		}).WithInProcess(true)

		// Create branch A
		if err := s.Scene.Repo.CreateChange("a change", "a", false); err != nil {
			t.Fatal(err)
		}
		s.RunCli("create", "a", "-m", "a change")

		// Create branch B on top of A
		if err := s.Scene.Repo.CreateChange("b change", "b", false); err != nil {
			t.Fatal(err)
		}
		s.RunCli("create", "b", "-m", "b change")

		// Run info --stack --json
		output, err := s.RunCliAndGetOutput("info", "--stack", "--json")

		require.NoError(t, err, "info command failed: %s", output)
		// Should be valid JSON and contain branch names
		require.Contains(t, output, "\"name\": \"a\"")
		require.Contains(t, output, "\"name\": \"b\"")
		require.Contains(t, output, "\"parent\": \"main\"")
		require.Contains(t, output, "\"parent\": \"a\"")
	})
}
