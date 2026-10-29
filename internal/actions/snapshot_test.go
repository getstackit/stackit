package actions_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/actions"
	"github.com/getstackit/stackit/internal/app/actiondeps"
	"github.com/getstackit/stackit/internal/config"
	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

func TestDisabledSnapshotClearsPreviousBinding(t *testing.T) {
	t.Parallel()
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
	s.WithInitialCommit()
	actiondeps.Snapshots(s.Context).Take(t.Context(), actions.NewSnapshot("create"))
	require.NotEmpty(t, s.Engine.LastSnapshotID())
	cfg, err := config.LoadConfig(s.Scene.Dir)
	require.NoError(t, err)
	require.NoError(t, cfg.SetUndoEnabled(false))
	s.Context.Config = cfg
	actiondeps.Snapshots(s.Context).Take(t.Context(), actions.NewSnapshot("modify"))
	require.Empty(t, s.Engine.LastSnapshotID())
	snapshots, err := s.Engine.Snapshots()
	require.NoError(t, err)
	require.Len(t, snapshots, 1)
}
