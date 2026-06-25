package cli_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/config"
	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

func TestInitCommand(t *testing.T) {
	t.Parallel()

	t.Run("can run init", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenarioParallel(t, testhelpers.InitialCommitSceneSetup).WithInProcess(true)

		// Remove existing config to test fresh init
		configPath := filepath.Join(s.Scene.Dir, ".git", ".stackit_config")
		_ = os.Remove(configPath)
		// Ignore error if file doesn't exist

		// Run init command
		output, err := s.RunCliAndGetOutput("init", "--trunk", "main")
		require.NoError(t, err, "init command failed: %s", output)

		normalized := testhelpers.NormalizeOutput(output)

		expected := testhelpers.NormalizeOutput(`
Welcome to Stackit!

Trunk set to main
Stackit initialized successfully!

Default configuration:
  - branch.pattern: {username}/{date}/{message}
  - submit.footer:  true
  - undo.depth:     10

Run 'stackit config' to change these settings.

Pro-tip: enhance your workflow with integrations:
  - GitHub:     stackit github install
  - Pre-commit: stackit precommit install
  - Agents:     stackit agent install
`)

		require.Equal(t, expected, normalized, "output format should match expected structure")

		// Verify config was created with correct trunk
		cfg := readRepoConfig(t, s.Scene.Dir)
		require.True(t, cfg.IsInitialized())
		require.Equal(t, "main", cfg.Trunk())
	})

	t.Run("errors on invalid trunk when explicitly provided", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenarioParallel(t, testhelpers.InitialCommitSceneSetup).WithInProcess(true)

		// Remove existing config
		configPath := filepath.Join(s.Scene.Dir, ".git", ".stackit_config")
		os.Remove(configPath)

		// Run init with non-existent branch
		output, err := s.RunCliAndGetOutput("init", "--trunk", "random")

		// Should fail with error about branch not found
		require.Error(t, err, "init should fail with invalid branch")
		require.Contains(t, output, "not found", "error message should mention branch not found")
	})

	t.Run("errors on invalid trunk when cannot infer", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenarioParallel(t, func(sc *testhelpers.Scene) error {
			// Create initial commit first
			if err := testhelpers.InitialCommitSceneSetup(sc); err != nil {
				return err
			}
			// Rename main branch to main2
			return sc.Repo.RunGitCommand("branch", "-m", "main2")
		}).WithInProcess(true)

		// Remove existing config
		configPath := filepath.Join(s.Scene.Dir, ".git", ".stackit_config")
		os.Remove(configPath)

		// Run init with non-existent branch and no way to infer
		output, err := s.RunCliAndGetOutput("init", "--trunk", "random")

		// Should fail with error
		require.Error(t, err, "init should fail when trunk cannot be inferred and invalid branch provided")
		require.Contains(t, output, "not found", "error message should mention branch not found")
	})

	t.Run("infers trunk when not provided", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenarioParallel(t, testhelpers.InitialCommitSceneSetup).WithInProcess(true)

		// Remove existing config
		configPath := filepath.Join(s.Scene.Dir, ".git", ".stackit_config")
		os.Remove(configPath)

		// Run init without specifying trunk (should infer main)
		output, err := s.RunCliAndGetOutput("init")
		require.NoError(t, err, "init command failed: %s", output)

		normalized := testhelpers.NormalizeOutput(output)

		expected := testhelpers.NormalizeOutput(`
Welcome to Stackit!

Trunk set to main
Stackit initialized successfully!

Default configuration:
  - branch.pattern: {username}/{date}/{message}
  - submit.footer:  true
  - undo.depth:     10

Run 'stackit config' to change these settings.

Pro-tip: enhance your workflow with integrations:
  - GitHub:     stackit github install
  - Pre-commit: stackit precommit install
  - Agents:     stackit agent install
`)

		require.Equal(t, expected, normalized, "output format should match expected structure")

		// Verify config was created with inferred trunk (main)
		cfg := readRepoConfig(t, s.Scene.Dir)
		require.True(t, cfg.IsInitialized())
		require.Equal(t, "main", cfg.Trunk())
	})

	t.Run("fails when not in git repository", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		s := scenario.NewScenarioParallel(t, nil).WithInProcess(true)
		s.Scene.Dir = tmpDir // Use empty dir instead of the one with git repo

		// Run init command in a non-git dir
		output, err := s.RunCliAndGetOutput("init")

		require.Error(t, err, "init should fail when not in git repository")
		require.Contains(t, output, "not a git repository")
	})
}

// readRepoConfig loads the repository config using the new git-based config
func readRepoConfig(t *testing.T, repoDir string) *config.GitConfig {
	t.Helper()

	cfg, err := config.LoadConfig(repoDir)
	require.NoError(t, err, "failed to load config")

	return cfg
}
