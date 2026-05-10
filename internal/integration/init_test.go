package integration

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/inprocess"
)

func TestInitIntegration(t *testing.T) {
	t.Parallel()

	t.Run("non-interactive mode shows integration hints", func(t *testing.T) {
		t.Parallel()

		// Create a fresh scene without pre-initialization
		scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)

		// Remove any existing config
		configPath := filepath.Join(scene.Dir, ".git", ".stackit_config")
		_ = os.Remove(configPath)

		// Run init with in-process CLI
		cli := inprocess.NewInProcessCLI()
		result := cli.Run(scene.Dir, "init", "--no-interactive")
		require.NoError(t, result.Err, "init should succeed: %s", result.Output)

		// Verify non-interactive hints are shown (not prompts)
		require.Contains(t, result.Output, "Pro-tip: enhance your workflow with integrations:")
		require.Contains(t, result.Output, "stackit github install")
		require.Contains(t, result.Output, "stackit precommit install")
		require.Contains(t, result.Output, "stackit agents install")
	})

	t.Run("init with reset reinitializes", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		// Run init with --reset
		sh.Run("init --reset").
			OutputContains("Reinitializing Stackit")
	})

	t.Run("config migration from JSON to git config", func(t *testing.T) {
		t.Parallel()

		// Create a fresh scene
		scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)

		// Create a legacy JSON config file
		configPath := filepath.Join(scene.Dir, ".git", ".stackit_config")
		jsonConfig := `{
			"trunk": "main",
			"submit.footer": false,
			"undo.depth": 15
		}`
		err := os.WriteFile(configPath, []byte(jsonConfig), 0600)
		require.NoError(t, err)

		// Run any stackit command - should trigger migration
		cli := inprocess.NewInProcessCLI()
		result := cli.Run(scene.Dir, "log")
		require.NoError(t, result.Err, "log should succeed: %s", result.Output)

		// Verify JSON file was moved to backup
		backupPath := filepath.Join(scene.Dir, ".git", ".stackit_config.migrated")
		_, err = os.Stat(backupPath)
		require.NoError(t, err, "backup file should exist after migration")

		// Verify original JSON file is gone
		_, err = os.Stat(configPath)
		require.True(t, os.IsNotExist(err), "original JSON config should be removed after migration")

		// Verify config values were migrated to git config
		result = cli.Run(scene.Dir, "config", "get", "submit.footer")
		require.NoError(t, result.Err)
		require.Contains(t, result.Output, "false")
	})

	t.Run("init creates git config instead of JSON", func(t *testing.T) {
		t.Parallel()

		// Create a fresh scene without initialization
		scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)

		// Remove any existing config
		configPath := filepath.Join(scene.Dir, ".git", ".stackit_config")
		_ = os.Remove(configPath)

		// Run init
		cli := inprocess.NewInProcessCLI()
		result := cli.Run(scene.Dir, "init", "--no-interactive")
		require.NoError(t, result.Err, "init should succeed: %s", result.Output)

		// Verify JSON config file was NOT created
		_, err := os.Stat(configPath)
		require.True(t, os.IsNotExist(err), "JSON config file should not be created")

		// Verify trunk is stored in git config by reading it directly
		cmd := exec.Command("git", "config", "--local", "stackit.trunk")
		cmd.Dir = scene.Dir
		out, err := cmd.Output()
		require.NoError(t, err, "git config should have stackit.trunk")
		require.Equal(t, "main", strings.TrimSpace(string(out)))
	})
}
