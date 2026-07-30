package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestRestackPreservesUncommittedChangesInWorktreeAnchor guards against a data-loss
// regression: restack used to run `git reset --hard HEAD` unconditionally against a
// managed worktree after moving its branch ref, discarding any uncommitted changes
// with no prompt, no warning, and no recovery path (the content lives in no git
// object, no stash, and undo snapshots only record refs).
//
// `stackit worktree create` with no --root-branch checks out the hidden anchor
// branch directly in the new worktree. Restacking that anchor (to fast-forward it
// to trunk) used to reset the worktree's working directory unconditionally, even
// when the user had uncommitted edits sitting there — exactly where uncommitted
// work tends to live in a freshly created worktree.
func TestRestackPreservesUncommittedChangesInWorktreeAnchor(t *testing.T) {
	t.Parallel()

	sh := NewTestShellInProcess(t)
	sh.SetWorktreeBasePath(t.TempDir())

	sh.Run("worktree create my-wt")
	sh.Run("worktree open my-wt")
	worktreePath := strings.TrimSpace(sh.Output())

	// Leave uncommitted, staged content in the worktree without creating any
	// branch under the anchor — the worktree stays checked out on the anchor
	// itself, matching `worktree create` with no --root-branch.
	sh.InWorktree(worktreePath).WriteFile("dirty.txt", "uncommitted work")

	anchorBranch := findWorktreeAnchor(t, sh)

	// Trunk advances, then restack fast-forwards the anchor to trunk.
	sh.Checkout("main").
		Commit("trunk1.txt", "chore: trunk commit").
		Run("restack --all-stacks")

	// The anchor still tracks trunk...
	sh.Git("rev-parse main")
	trunkRev := strings.TrimSpace(sh.Output())
	sh.Git("rev-parse " + anchorBranch)
	require.Equal(t, trunkRev, strings.TrimSpace(sh.Output()), "anchor must still fast-forward to trunk")

	// ...but the uncommitted work in its worktree must survive untouched.
	content, err := os.ReadFile(filepath.Join(worktreePath, "dirty.txt"))
	require.NoError(t, err, "uncommitted file must not be deleted by restack")
	require.Equal(t, "uncommitted work", string(content), "uncommitted file content must be preserved")

	wtShell := sh.InWorktree(worktreePath).Git("status --porcelain")
	require.Contains(t, wtShell.Output(), "dirty.txt", "worktree must still report the uncommitted change")
}
