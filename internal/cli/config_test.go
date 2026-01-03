package cli_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"stackit.dev/stackit/testhelpers"
	"stackit.dev/stackit/testhelpers/scenario"
)

func TestConfigCommand(t *testing.T) {
	t.Parallel()

	t.Run("config get returns default pattern when not set", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).WithInProcess(true)

		// Get branch.pattern (should return default)
		output, err := s.RunCliAndGetOutput("config", "get", "branch.pattern")
		require.NoError(t, err, "config get command failed: %s", output)

		// Should return default pattern
		require.Equal(t, "{username}/{date}/{message}", strings.TrimSpace(output))
	})

	t.Run("config set and get branch.pattern", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).WithInProcess(true)

		// Set a custom pattern
		pattern := "{username}/{date}/{message}"
		output, err := s.RunCliAndGetOutput("config", "set", "branch.pattern", pattern)
		require.NoError(t, err, "config set command failed: %s", output)
		require.Contains(t, output, "Set branch.pattern to:")

		// Get the pattern back
		output, err = s.RunCliAndGetOutput("config", "get", "branch.pattern")
		require.NoError(t, err, "config get command failed: %s", output)
		require.Equal(t, pattern, strings.TrimSpace(output))
	})

	t.Run("config set rejects pattern without message placeholder", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).WithInProcess(true)

		// Try to set a pattern without {message}
		output, err := s.RunCliAndGetOutput("config", "set", "branch.pattern", "{username}/{date}")
		require.Error(t, err, "config set should fail without {message} placeholder")
		require.Contains(t, output, "must contain {message}")

		// Verify pattern was not set (should still be default)
		output, err = s.RunCliAndGetOutput("config", "get", "branch.pattern")
		require.NoError(t, err)
		require.Equal(t, "{username}/{date}/{message}", strings.TrimSpace(output))
	})

	t.Run("config set accepts pattern with only message placeholder", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).WithInProcess(true)

		// Set pattern with only {message}
		pattern := "{message}"
		output, err := s.RunCliAndGetOutput("config", "set", "branch.pattern", pattern)
		require.NoError(t, err, "config set command failed: %s", output)

		// Verify it was set
		output, err = s.RunCliAndGetOutput("config", "get", "branch.pattern")
		require.NoError(t, err)
		require.Equal(t, pattern, strings.TrimSpace(output))
	})

	t.Run("config get fails for unknown key", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).WithInProcess(true)

		// Try to get unknown key
		output, err := s.RunCliAndGetOutput("config", "get", "unknown-key")
		require.Error(t, err, "config get should fail for unknown key")
		require.Contains(t, output, "unknown configuration key")
	})

	t.Run("config set fails for unknown key", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).WithInProcess(true)

		// Try to set unknown key
		output, err := s.RunCliAndGetOutput("config", "set", "unknown-key", "value")
		require.Error(t, err, "config set should fail for unknown key")
		require.Contains(t, output, "unknown configuration key")
	})

	t.Run("config get fails when not in git repository", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		s := scenario.NewScenarioParallel(t, nil).WithInProcess(true)
		s.Scene.Dir = tmpDir // Use empty dir instead of the one with git repo

		// Don't initialize git or stackit - just try to run config get
		output, err := s.RunCliAndGetOutput("config", "get", "branch.pattern")
		require.Error(t, err, "config get should fail when not in git repository")
		require.Contains(t, output, "not a git repository")
	})

	t.Run("config set fails when not in git repository", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		s := scenario.NewScenarioParallel(t, nil).WithInProcess(true)
		s.Scene.Dir = tmpDir // Use empty dir instead of the one with git repo

		// Don't initialize git or stackit - just try to run config set
		output, err := s.RunCliAndGetOutput("config", "set", "branch.pattern", "{message}")
		require.Error(t, err, "config set should fail when not in git repository")
		require.Contains(t, output, "not a git repository")
	})

	t.Run("config set persists pattern across commands", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).WithInProcess(true)

		// Set a custom pattern
		pattern := "{username}/dev/{date}/{message}"
		s.RunCli("config", "set", "branch.pattern", pattern)

		// Get it back
		output, err := s.RunCliAndGetOutput("config", "get", "branch.pattern")
		require.NoError(t, err)
		require.Equal(t, pattern, strings.TrimSpace(output))

		// Set a different pattern
		pattern2 := "{date}/{message}"
		s.RunCli("config", "set", "branch.pattern", pattern2)

		// Verify it changed
		output, err = s.RunCliAndGetOutput("config", "get", "branch.pattern")
		require.NoError(t, err)
		require.Equal(t, pattern2, strings.TrimSpace(output))
	})

	t.Run("config get submit.footer returns true by default", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).WithInProcess(true)

		// Get submit.footer (should return true by default)
		output, err := s.RunCliAndGetOutput("config", "get", "submit.footer")
		require.NoError(t, err, "config get command failed: %s", output)

		// Should return true
		require.Equal(t, "true", strings.TrimSpace(output))
	})

	t.Run("config set and get submit.footer", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).WithInProcess(true)

		// Set submit.footer to false
		output, err := s.RunCliAndGetOutput("config", "set", "submit.footer", "false")
		require.NoError(t, err, "config set command failed: %s", output)
		require.Contains(t, output, "Set submit.footer to:")

		// Get submit.footer back
		output, err = s.RunCliAndGetOutput("config", "get", "submit.footer")
		require.NoError(t, err, "config get command failed: %s", output)
		require.Equal(t, "false", strings.TrimSpace(output))

		// Set submit.footer to true
		output, err = s.RunCliAndGetOutput("config", "set", "submit.footer", "true")
		require.NoError(t, err, "config set command failed: %s", output)

		// Get submit.footer back
		output, err = s.RunCliAndGetOutput("config", "get", "submit.footer")
		require.NoError(t, err, "config get command failed: %s", output)
		require.Equal(t, "true", strings.TrimSpace(output))
	})
}
