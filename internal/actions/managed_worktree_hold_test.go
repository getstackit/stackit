package actions

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/engine"
)

type stubInspector struct {
	rebasing   bool
	hasChanges bool
	err        error
}

func (s stubInspector) WorktreeRebaseInProgress(context.Context, engine.WorktreePath) bool {
	return s.rebasing
}

func (s stubInspector) WorktreeHasUncommittedChanges(context.Context, engine.WorktreePath) (bool, error) {
	return s.hasChanges, s.err
}

// TestManagedWorktreeHoldReason pins the decision sync, its --dry-run preview,
// and restack all read before touching a managed worktree's stack.
//
// The dirty probe's error used to be discarded, so a worktree git could not
// read — an index.lock held by a concurrent process, unreadable permissions —
// came back "clean" and the restack moved refs under a checkout whose contents
// were unknown. That is exactly the state the skip exists to prevent.
func TestManagedWorktreeHoldReason(t *testing.T) {
	t.Parallel()

	existing := engine.WorktreePath(t.TempDir())

	t.Run("clean worktree is safe", func(t *testing.T) {
		t.Parallel()
		require.Empty(t, ManagedWorktreeHoldReason(context.Background(), stubInspector{}, existing))
	})

	t.Run("dirty worktree is skipped", func(t *testing.T) {
		t.Parallel()
		reason := ManagedWorktreeHoldReason(context.Background(), stubInspector{hasChanges: true}, existing)
		require.Contains(t, reason, "uncommitted changes")
	})

	t.Run("uninspectable worktree is skipped", func(t *testing.T) {
		t.Parallel()
		reason := ManagedWorktreeHoldReason(context.Background(),
			stubInspector{err: errors.New("index.lock exists")}, existing)
		require.Contains(t, reason, "could not be inspected")
		require.Contains(t, reason, "index.lock")
	})

	t.Run("rebase in progress is skipped", func(t *testing.T) {
		t.Parallel()
		// stackit's own conflict workflow leaves the worktree mid-rebase with a
		// clean index; resetting it would abandon the rebase the user is in.
		reason := ManagedWorktreeHoldReason(context.Background(), stubInspector{rebasing: true}, existing)
		require.Equal(t, restackWorktreeHoldReasonRebase, reason)
	})

	t.Run("missing directory is not skipped", func(t *testing.T) {
		t.Parallel()
		// Nothing left to protect, and skipping would block the orphan cleanup
		// whose whole job is retiring this registration. The probe would fail
		// here, so this must not be lumped in with uninspectable worktrees.
		missing := engine.WorktreePath(filepath.Join(t.TempDir(), "gone"))
		reason := ManagedWorktreeHoldReason(context.Background(),
			stubInspector{err: errors.New("no such file or directory")}, missing)
		require.Empty(t, reason)
	})
}
