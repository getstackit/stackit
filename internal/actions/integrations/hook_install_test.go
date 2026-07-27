package integrations

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/output"
)

// One shared helper backs both the pre-commit and pre-push hooks, so a bug in
// the marker-stripping loop below would break both at once. These tests use
// the pre-commit constants; the pre-push path differs only in those values.

// hookRepo returns a temp repo root and the path the hook will be written to.
func hookRepo(t *testing.T) (repoRoot, hookPath string) {
	t.Helper()
	repoRoot = t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(repoRoot, ".git", "hooks"), 0o750))
	return repoRoot, filepath.Join(repoRoot, ".git", "hooks", precommitHookName)
}

func installPrecommit(t *testing.T, repoRoot string) string {
	t.Helper()
	out := output.NewTestOutput()
	require.NoError(t, installHook(repoRoot, precommitHookName, precommitMarker, precommitHookTemplate, precommitDisplayName, out))
	return out.String()
}

func uninstallPrecommit(t *testing.T, repoRoot string) string {
	t.Helper()
	out := output.NewTestOutput()
	require.NoError(t, uninstallHook(out, repoRoot, precommitHookName, precommitMarker, precommitDisplayName))
	return out.String()
}

func readHook(t *testing.T, hookPath string) string {
	t.Helper()
	content, err := os.ReadFile(hookPath) // #nosec G304 - test-controlled path
	require.NoError(t, err)
	return string(content)
}

func TestInstallHook(t *testing.T) {
	t.Parallel()

	t.Run("writes the template when no hook exists", func(t *testing.T) {
		t.Parallel()
		repoRoot, hookPath := hookRepo(t)

		logged := installPrecommit(t, repoRoot)

		require.Equal(t, precommitHookTemplate, readHook(t, hookPath))
		require.Contains(t, logged, "Installed Stackit pre-commit hook.")

		// Git ignores a hook it cannot execute, so the mode is load-bearing.
		info, err := os.Stat(hookPath)
		require.NoError(t, err)
		require.NotZero(t, info.Mode().Perm()&0o100, "hook must be executable")
	})

	t.Run("creates the hooks directory when missing", func(t *testing.T) {
		t.Parallel()
		repoRoot := t.TempDir()
		hookPath := filepath.Join(repoRoot, ".git", "hooks", precommitHookName)

		installPrecommit(t, repoRoot)

		require.Equal(t, precommitHookTemplate, readHook(t, hookPath))
	})

	t.Run("appends to an existing foreign hook without clobbering it", func(t *testing.T) {
		t.Parallel()
		repoRoot, hookPath := hookRepo(t)
		foreign := "#!/bin/bash\necho \"someone else's hook\"\n"
		require.NoError(t, os.WriteFile(hookPath, []byte(foreign), 0o750)) // #nosec G306 - hooks are executable

		logged := installPrecommit(t, repoRoot)

		got := readHook(t, hookPath)
		require.Contains(t, got, "echo \"someone else's hook\"", "existing hook body must survive")
		require.Contains(t, got, "# Added by Stackit")
		require.Contains(t, got, precommitMarker)
		require.Contains(t, logged, "Appended Stackit verification to existing pre-commit hook.")
	})

	t.Run("is idempotent when already installed", func(t *testing.T) {
		t.Parallel()
		repoRoot, hookPath := hookRepo(t)

		installPrecommit(t, repoRoot)
		first := readHook(t, hookPath)
		logged := installPrecommit(t, repoRoot)

		require.Equal(t, first, readHook(t, hookPath), "second install must not duplicate the marker")
		require.Contains(t, logged, "Pre-commit hook is already installed.")
	})
}

func TestUninstallHook(t *testing.T) {
	t.Parallel()

	t.Run("removes a hook stackit installed", func(t *testing.T) {
		t.Parallel()
		repoRoot, hookPath := hookRepo(t)
		installPrecommit(t, repoRoot)

		logged := uninstallPrecommit(t, repoRoot)

		// Nothing but a shebang would be left, so the file goes entirely.
		require.NoFileExists(t, hookPath)
		require.Contains(t, logged, "Removed Stackit pre-commit hook.")
	})

	t.Run("strips only the stackit lines from a foreign hook", func(t *testing.T) {
		t.Parallel()
		repoRoot, hookPath := hookRepo(t)
		foreign := "#!/bin/bash\necho \"someone else's hook\"\n"
		require.NoError(t, os.WriteFile(hookPath, []byte(foreign), 0o750)) // #nosec G306 - hooks are executable
		installPrecommit(t, repoRoot)

		logged := uninstallPrecommit(t, repoRoot)

		got := readHook(t, hookPath)
		require.Contains(t, got, "echo \"someone else's hook\"", "the other tool's hook must survive")
		require.NotContains(t, got, precommitMarker)
		require.NotContains(t, got, "# Added by Stackit", "the marker's comment goes with it")
		require.Contains(t, logged, "Removed Stackit verification from existing pre-commit hook.")
	})

	t.Run("reports when no hook file exists", func(t *testing.T) {
		t.Parallel()
		repoRoot, _ := hookRepo(t)

		logged := uninstallPrecommit(t, repoRoot)

		require.Contains(t, logged, "Pre-commit hook is not installed.")
	})

	t.Run("leaves a foreign hook alone when the marker is absent", func(t *testing.T) {
		t.Parallel()
		repoRoot, hookPath := hookRepo(t)
		foreign := "#!/bin/bash\necho \"someone else's hook\"\n"
		require.NoError(t, os.WriteFile(hookPath, []byte(foreign), 0o750)) // #nosec G306 - hooks are executable

		logged := uninstallPrecommit(t, repoRoot)

		require.Equal(t, foreign, readHook(t, hookPath), "an unrelated hook must not be rewritten")
		require.Contains(t, logged, "Stackit verification not found in pre-commit hook.")
	})
}
