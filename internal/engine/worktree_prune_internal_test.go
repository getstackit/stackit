package engine

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/git"
)

type failingPruneRunner struct {
	git.Runner
	calls int
}

func (g *failingPruneRunner) PruneWorktrees(context.Context) error {
	g.calls++
	return errors.New("prune failed")
}

func TestTemporaryWorktreePruneFailureIsRetried(t *testing.T) {
	t.Parallel()
	g := &failingPruneRunner{}
	e := &engineImpl{git: g, tempWorktreeNeedsPrune: true}

	e.maybePruneTempWorktreesLocked(t.Context(), WorktreePruneAuto)
	e.maybePruneTempWorktreesLocked(t.Context(), WorktreePruneAuto)

	require.Equal(t, 2, g.calls, "a failed prune must not suppress the next attempt")
	require.True(t, e.tempWorktreeNeedsPrune)
	require.False(t, e.tempWorktreePrunedOnce)
}
