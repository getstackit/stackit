package worktree

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/output"
)

// post-worktree-create hooks are arbitrary user-configured shell commands whose
// whole purpose is to mutate a fresh worktree. os/exec treats an empty Dir as
// the current directory, so passing an empty worktree path through would run
// them against the user's main checkout instead. The guard returns before
// hooks.Run, and the warning is the observable proof it fired.
func TestRunResolvedHooksWarnsWhenSkipped(t *testing.T) {
	t.Parallel()

	out := output.NewTestOutput()
	RunResolvedHooks(context.Background(), []string{"echo hi"}, "", out)

	require.Contains(t, out.String(), "Skipped post-worktree-create hooks")
}

// Nothing configured means nothing to warn about.
func TestRunResolvedHooksSilentWithNoHooks(t *testing.T) {
	t.Parallel()

	out := output.NewTestOutput()
	RunResolvedHooks(context.Background(), nil, "", out)

	require.NotContains(t, out.String(), "Skipped post-worktree-create hooks")
}

// The ordinary path still runs the hook, in the directory it was given.
func TestRunResolvedHooksRunsInWorktree(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	out := output.NewTestOutput()
	RunResolvedHooks(context.Background(), []string{"touch ran.txt"}, dir, out)

	require.FileExists(t, filepath.Join(dir, "ran.txt"))
}
