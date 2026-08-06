package merge

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/internal/output"
)

// recordingRemoveEngine records RemoveWorktree calls so a test can assert that
// a destructive call was never attempted.
type recordingRemoveEngine struct {
	mergeExecuteEngine
	removed []string
}

func (e *recordingRemoveEngine) RemoveWorktree(_ context.Context, path string) error {
	e.removed = append(e.removed, path)
	return nil
}

// TestRemoveWorktreeForBranch covers freeing a branch's checkout before the
// merge plan deletes it.
//
// A consolidation merge runs its steps with an engine rooted in a temporary
// worktree. The main-worktree guard used to compare the candidate path against
// that engine's repo root, so the user's main checkout never matched and
// `git worktree remove <main repo>` was attempted — git refuses, but only after
// the attempt, and the branch delete that followed then failed too. Both showed
// up as warnings about a failure the user could do nothing about.
func TestRemoveWorktreeForBranch(t *testing.T) {
	t.Parallel()

	mainRepo := t.TempDir()
	linked := t.TempDir()
	worktrees := git.WorktreeList{
		{Path: mainRepo, Branch: "landed"},
		{Path: linked, Branch: "feature"},
	}
	out := output.NewNullOutput()

	t.Run("refuses the main working tree without calling git", func(t *testing.T) {
		t.Parallel()
		eng := &recordingRemoveEngine{}

		err := removeWorktreeForBranch(context.Background(), "landed", worktrees, eng, out)

		require.ErrorIs(t, err, errBranchInMainWorktree,
			"caller relies on this sentinel to defer the branch to post-merge cleanup")
		require.Empty(t, eng.removed, "must not ask git to remove a main working tree")
	})

	t.Run("removes a linked worktree", func(t *testing.T) {
		t.Parallel()
		eng := &recordingRemoveEngine{}

		require.NoError(t, removeWorktreeForBranch(context.Background(), "feature", worktrees, eng, out))
		require.Equal(t, []string{linked}, eng.removed)
	})

	t.Run("branch in no worktree needs nothing removed", func(t *testing.T) {
		t.Parallel()
		eng := &recordingRemoveEngine{}

		require.NoError(t, removeWorktreeForBranch(context.Background(), "untouched", worktrees, eng, out))
		require.Empty(t, eng.removed)
	})

	t.Run("sentinel is distinguishable from a real removal failure", func(t *testing.T) {
		t.Parallel()
		// The caller treats only the sentinel as "not a problem"; anything else
		// still warns.
		require.False(t, errors.Is(errors.New("failed to remove worktree"), errBranchInMainWorktree))
	})
}
