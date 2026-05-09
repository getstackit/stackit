package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"stackit.dev/stackit/testhelpers"
	"stackit.dev/stackit/testhelpers/inprocess"
)

func TestConfigInit(t *testing.T) {
	t.Parallel()

	t.Run("creates .stackit.yaml when none exists", func(t *testing.T) {
		t.Parallel()

		scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)

		// Ensure no .stackit.yaml exists
		configPath := filepath.Join(scene.Dir, ".stackit.yaml")
		_ = os.Remove(configPath)

		// Run config init
		cli := inprocess.NewInProcessCLI()
		result := cli.Run(scene.Dir, "config", "init")
		require.NoError(t, result.Err, "config init should succeed: %s", result.Output)

		// Verify file was created
		content, err := os.ReadFile(configPath)
		require.NoError(t, err, ".stackit.yaml should be created")

		// Verify it's a commented template
		require.Contains(t, string(content), "# Stackit Team Configuration")
		require.Contains(t, string(content), "# trunk:")
		require.Contains(t, string(content), "# submit:")

		// Verify success message
		require.Contains(t, result.Output, "Created .stackit.yaml")
	})

	t.Run("errors when file exists without force flag", func(t *testing.T) {
		t.Parallel()

		scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)

		// Create existing .stackit.yaml
		configPath := filepath.Join(scene.Dir, ".stackit.yaml")
		err := os.WriteFile(configPath, []byte("trunk: main\n"), 0644)
		require.NoError(t, err)

		// Run config init without force
		cli := inprocess.NewInProcessCLI()
		result := cli.Run(scene.Dir, "config", "init")
		require.Error(t, result.Err, "config init should fail when file exists")
		require.Contains(t, result.Output, "already exists")
		require.Contains(t, result.Output, "--force")

		// Verify original file is unchanged
		content, err := os.ReadFile(configPath)
		require.NoError(t, err)
		require.Equal(t, "trunk: main\n", string(content))
	})

	t.Run("overwrites with force flag", func(t *testing.T) {
		t.Parallel()

		scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)

		// Create existing .stackit.yaml
		configPath := filepath.Join(scene.Dir, ".stackit.yaml")
		err := os.WriteFile(configPath, []byte("trunk: main\n"), 0644)
		require.NoError(t, err)

		// Run config init with force
		cli := inprocess.NewInProcessCLI()
		result := cli.Run(scene.Dir, "config", "init", "--force")
		require.NoError(t, result.Err, "config init --force should succeed: %s", result.Output)

		// Verify file was overwritten with template
		content, err := os.ReadFile(configPath)
		require.NoError(t, err)
		require.Contains(t, string(content), "# Stackit Team Configuration")
		require.NotEqual(t, "trunk: main\n", string(content))
	})

	t.Run("errors outside git repository", func(t *testing.T) {
		t.Parallel()

		// Create a non-git directory
		tmpDir, err := os.MkdirTemp("", "stackit-test-*")
		require.NoError(t, err)
		t.Cleanup(func() { os.RemoveAll(tmpDir) })

		// Run config init
		cli := inprocess.NewInProcessCLI()
		result := cli.Run(tmpDir, "config", "init")
		require.Error(t, result.Err, "config init should fail outside git repo")
		require.Contains(t, result.Output, "not a git repository")
	})

	t.Run("template contains all expected sections", func(t *testing.T) {
		t.Parallel()

		scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)

		// Run config init
		cli := inprocess.NewInProcessCLI()
		result := cli.Run(scene.Dir, "config", "init")
		require.NoError(t, result.Err)

		// Read the created file
		configPath := filepath.Join(scene.Dir, ".stackit.yaml")
		content, err := os.ReadFile(configPath)
		require.NoError(t, err)

		// Verify key sections are present
		contentStr := string(content)
		expectedSections := []string{
			"# trunk:",
			"# trunks:",
			"# branch:",
			"# submit:",
			"# merge:",
			"# ci:",
			"# navigation:",
			"# hooks:",
		}

		for _, section := range expectedSections {
			require.True(t, strings.Contains(contentStr, section),
				"Template should contain section: %s", section)
		}

		// Verify docs URL
		require.Contains(t, contentStr, "https://getstackit.github.io/stackit/cli/config/")
	})
}
