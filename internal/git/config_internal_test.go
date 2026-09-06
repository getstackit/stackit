package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestNormalizeConfigKey pins the key rendering the stackit.* snapshot relies
// on. `git config --get-regexp` lowercases the section and the trailing key
// name but preserves the subsection verbatim; if this drifts, Get silently
// returns "" for a key that is actually set.
func TestNormalizeConfigKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		key  string
		want string
	}{
		{"already lowercase", "stackit.trunk", "stackit.trunk"},
		{"mixed-case key name", "stackit.maxConcurrency", "stackit.maxconcurrency"},
		{"subsection with mixed-case key", "stackit.worktree.basePath", "stackit.worktree.basepath"},
		{"lowercase subsection preserved", "stackit.undo.depth", "stackit.undo.depth"},
		{"subsection case is significant", "branch.Feature-A.remote", "branch.Feature-A.remote"},
		{"subsection may contain dots", "branch.a.b/c.remote", "branch.a.b/c.remote"},
		{"no separator", "nodots", "nodots"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, normalizeConfigKey(tt.key))
		})
	}
}

// TestSharedSnapshotSeesOutOfBandWrite pins the invalidation that makes the
// repo-scoped snapshot safe to share across ConfigStores. A write made with
// plain `git config` (a hook, a test helper, the user's editor) never bumps
// configGen, so only the config file's stamp can stale the shared entry — if
// that stops working, a fresh ConfigStore serves the pre-write value forever.
func TestSharedSnapshotSeesOutOfBandWrite(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	runGit := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, out)
	}
	runGit("init", "-q", ".")
	runGit("config", "stackit.trunk", "main")

	first, err := NewConfigStore(dir).Get("stackit.trunk")
	require.NoError(t, err)
	require.Equal(t, "main", first)

	// Out of band: not routed through ConfigStore, so configGen is unchanged.
	runGit("config", "stackit.trunk", "develop")

	second, err := NewConfigStore(dir).Get("stackit.trunk")
	require.NoError(t, err)
	require.Equal(t, "develop", second, "fresh store served a stale shared snapshot")

	// And the store that already cached the old value must re-read too.
	store := NewConfigStore(dir)
	_, err = store.Get("stackit.trunk")
	require.NoError(t, err)
	runGit("config", "stackit.trunk", "trunk")
	third, err := store.Get("stackit.trunk")
	require.NoError(t, err)
	require.Equal(t, "trunk", third, "existing store served a stale per-store snapshot")
}

// TestSharedSnapshotSeesGlobalWrite pins the other half of the stamp. The
// snapshot is built with `git config --get-regexp` and no scope flag, so it
// merges global config as well as local. Stamping only the local file would
// let the repo-scoped shared snapshot serve a stale global value for the whole
// process — invisible in the CLI, but the server builds a ConfigStore per call
// and outlives any number of edits to the user's ~/.gitconfig.
func TestSharedSnapshotSeesGlobalWrite(t *testing.T) {
	// Mutates GIT_CONFIG_GLOBAL for the process, so it cannot run in parallel.
	dir := t.TempDir()
	globalPath := filepath.Join(t.TempDir(), "gitconfig")

	runGit := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+globalPath)
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, out)
	}

	t.Setenv("GIT_CONFIG_GLOBAL", globalPath)
	runGit("init", "-q", ".")
	runGit("config", "--global", "stackit.trunk", "main")

	first, err := NewConfigStore(dir).Get("stackit.trunk")
	require.NoError(t, err)
	require.Equal(t, "main", first)

	// Global-only write: the local config file is untouched, so a stamp that
	// covers only the local file cannot notice this.
	runGit("config", "--global", "stackit.trunk", "develop")

	second, err := NewConfigStore(dir).Get("stackit.trunk")
	require.NoError(t, err)
	require.Equal(t, "develop", second, "fresh store served a snapshot stale in global scope")
}
