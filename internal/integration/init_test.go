package integration

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/inprocess"
)

func TestInitIntegration(t *testing.T) {
	t.Parallel()

	t.Run("init with reset reinitializes", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		sh.Run("init --reset").
			OutputContains("Reinitializing Stackit")
	})

	t.Run("config migration from JSON to git config", func(t *testing.T) {
		t.Parallel()

		scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)

		configPath := filepath.Join(scene.Dir, ".git", ".stackit_config")
		jsonConfig := `{
			"trunk": "main",
			"submit.footer": false,
			"undo.depth": 15
		}`
		err := os.WriteFile(configPath, []byte(jsonConfig), 0600)
		require.NoError(t, err)

		// Any stackit command should trigger migration.
		cli := inprocess.NewInProcessCLI()
		result := cli.Run(scene.Dir, "log")
		require.NoError(t, result.Err, "log should succeed: %s", result.Output)

		backupPath := filepath.Join(scene.Dir, ".git", ".stackit_config.migrated")
		_, err = os.Stat(backupPath)
		require.NoError(t, err, "backup file should exist after migration")

		_, err = os.Stat(configPath)
		require.True(t, os.IsNotExist(err), "original JSON config should be removed after migration")

		result = cli.Run(scene.Dir, "config", "get", "submit.footer")
		require.NoError(t, result.Err)
		require.Contains(t, result.Output, "false")
	})
}
