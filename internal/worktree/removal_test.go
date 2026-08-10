package worktree

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

func TestRemovalPolicyDiscardsChanges(t *testing.T) {
	t.Parallel()

	require.False(t, RemovalRespectChanges.DiscardsChanges())
	require.True(t, RemovalDiscardChanges.DiscardsChanges())
}

func TestRemovePath_MissingPathDoesNotFail(t *testing.T) {
	t.Parallel()
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
	s.WithInitialCommit()

	removed, err := RemovePath(context.Background(), s.Engine, filepath.Join(t.TempDir(), "missing"), RemovalRespectChanges)
	require.NoError(t, err)
	require.False(t, removed)
}
