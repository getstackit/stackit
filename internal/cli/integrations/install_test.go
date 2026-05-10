package integrations

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/git"
)

// Not parallel: subtests use t.Setenv.
func TestIsAgentsInstalled(t *testing.T) {
	t.Run("detects global codex installation", func(t *testing.T) {
		homeDir := t.TempDir()
		t.Setenv("HOME", homeDir)

		skillPath := filepath.Join(homeDir, ".codex", "skills", "stackit", "SKILL.md")
		err := os.MkdirAll(filepath.Dir(skillPath), 0750)
		require.NoError(t, err)
		err = os.WriteFile(skillPath, []byte("skill"), 0600)
		require.NoError(t, err)

		runner := git.NewRunnerWithPath(t.TempDir(), nil)
		require.True(t, IsAgentsInstalled(runner))
	})

	t.Run("detects local claude installation", func(t *testing.T) {
		homeDir := t.TempDir()
		t.Setenv("HOME", homeDir)

		repoRoot := t.TempDir()
		cmd := exec.Command("git", "init", repoRoot)
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, string(out))

		skillPath := filepath.Join(repoRoot, ".claude", "skills", "stackit", "SKILL.md")
		err = os.MkdirAll(filepath.Dir(skillPath), 0750)
		require.NoError(t, err)
		err = os.WriteFile(skillPath, []byte("skill"), 0600)
		require.NoError(t, err)

		runner := git.NewRunnerWithPath(repoRoot, nil)
		require.True(t, IsAgentsInstalled(runner))
	})

	t.Run("returns false when no installation exists", func(t *testing.T) {
		homeDir := t.TempDir()
		t.Setenv("HOME", homeDir)

		runner := git.NewRunnerWithPath(t.TempDir(), nil)
		require.False(t, IsAgentsInstalled(runner))
	})
}

// Not parallel: subtests use t.Setenv.
func TestAutoDetectFormats(t *testing.T) {
	t.Run("defaults to both formats when no dirs exist", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		require.Equal(t, []string{"claude", "codex"}, autoDetectFormats())
	})

	t.Run("detects both dirs", func(t *testing.T) {
		homeDir := t.TempDir()
		t.Setenv("HOME", homeDir)
		require.NoError(t, os.MkdirAll(filepath.Join(homeDir, ".claude"), 0750))
		require.NoError(t, os.MkdirAll(filepath.Join(homeDir, ".codex"), 0750))
		require.Equal(t, []string{"claude", "codex"}, autoDetectFormats())
	})

	t.Run("detects only codex", func(t *testing.T) {
		homeDir := t.TempDir()
		t.Setenv("HOME", homeDir)
		require.NoError(t, os.MkdirAll(filepath.Join(homeDir, ".codex"), 0750))
		require.Equal(t, []string{"codex"}, autoDetectFormats())
	})
}
