package sync

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

type stubInspector struct {
	hasChanges bool
	err        error
}

func (s stubInspector) WorktreeHasUncommittedChanges(context.Context, string) (bool, error) {
	return s.hasChanges, s.err
}

// TestSkipReasonForWorktree pins the decision sync and its --dry-run preview
// both read before touching a managed worktree's stack.
//
// The dirty probe's error used to be discarded, so a worktree git could not
// read — an index.lock held by a concurrent process, unreadable permissions —
// came back "clean" and the restack moved refs under a checkout whose contents
// were unknown. That is exactly the state the skip exists to prevent.
func TestSkipReasonForWorktree(t *testing.T) {
	t.Parallel()

	existing := t.TempDir()

	t.Run("clean worktree is safe", func(t *testing.T) {
		t.Parallel()
		require.Empty(t, SkipReasonForWorktree(context.Background(), stubInspector{}, existing))
	})

	t.Run("dirty worktree is skipped", func(t *testing.T) {
		t.Parallel()
		reason := SkipReasonForWorktree(context.Background(), stubInspector{hasChanges: true}, existing)
		require.Contains(t, reason, "uncommitted changes")
	})

	t.Run("uninspectable worktree is skipped", func(t *testing.T) {
		t.Parallel()
		reason := SkipReasonForWorktree(context.Background(),
			stubInspector{err: errors.New("index.lock exists")}, existing)
		require.Contains(t, reason, "cannot inspect")
		require.Contains(t, reason, "index.lock")
	})

	t.Run("missing directory is not skipped", func(t *testing.T) {
		t.Parallel()
		// Nothing left to protect, and skipping would block the orphan cleanup
		// whose whole job is retiring this registration. The probe would fail
		// here, so this must not be lumped in with uninspectable worktrees.
		missing := filepath.Join(t.TempDir(), "gone")
		reason := SkipReasonForWorktree(context.Background(),
			stubInspector{err: errors.New("no such file or directory")}, missing)
		require.Empty(t, reason)
	})
}
