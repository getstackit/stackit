package engine

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/git"
)

// blindWorktreeRunner wraps a real git.Runner and fails ListWorktrees, standing
// in for any repo where the physical checkout map cannot be read — most
// concretely a Git older than 2.36, which does not accept `worktree list -z`.
type blindWorktreeRunner struct {
	git.Runner
}

func (r *blindWorktreeRunner) ListWorktrees(context.Context) (git.WorktreeList, error) {
	return nil, errors.New("injected: cannot list worktrees")
}

// TestRestackFailsLoudlyWhenWorktreesCannotBeInspected covers the reporting half
// of the worktree guard.
//
// Without the checkout map, no ref move is safe, so every branch is held — and a
// held branch returns RestackUnneeded, which callers render as "up to date".
// That turned an unreadable worktree list into modify/sync/squash/absorb
// reporting success while leaving the entire stack unrestacked. Refusing is the
// only honest answer; the one thing this must not do is claim there was nothing
// to do.
func TestRestackFailsLoudlyWhenWorktreesCannotBeInspected(t *testing.T) {
	t.Parallel()

	dir, realGit := newOptimisticLockTestRepo(t)
	ctx := context.Background()

	trackingEngine, err := NewEngine(Options{RepoRoot: dir, Trunk: "main", Git: realGit})
	require.NoError(t, err)
	trackingImpl, ok := trackingEngine.(*engineImpl)
	require.True(t, ok)
	require.NoError(t, trackingImpl.TrackBranch(ctx, "feature", "main"))

	blindGit := &blindWorktreeRunner{Runner: realGit}
	eng, err := NewEngine(Options{RepoRoot: dir, Trunk: "main", Git: blindGit})
	require.NoError(t, err)

	_, err = eng.RestackBranches(ctx, BranchesFromNames(eng, []string{"feature"}))
	require.Error(t, err, "an uninspectable worktree list must not be reported as nothing to do")
	require.Contains(t, err.Error(), "inspecting Git worktrees")
}
