package provision_test

import (
	"context"
	"testing"

	"github.com/getstackit/stackit/internal/api/provision"
	"github.com/getstackit/stackit/internal/api/registry"
	"github.com/getstackit/stackit/testhelpers"
	"github.com/stretchr/testify/require"
)

// TestBuildEntry verifies the shared build path produces a fully wired entry
// (engine + broadcaster + watcher) that the registry can adopt and close.
func TestBuildEntry(t *testing.T) {
	t.Parallel()
	scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)

	entry, err := provision.BuildEntry(context.Background(), provision.EntryParams{
		ID:          "owner-repo",
		DisplayName: "owner/repo",
		Path:        scene.Dir,
		Remote:      "origin",
	})
	require.NoError(t, err)
	require.NotNil(t, entry)

	// Adopt into a registry so its closers (watcher goroutine) tear down.
	reg := registry.New()
	require.NoError(t, reg.Add(entry))
	t.Cleanup(func() { _ = reg.Close() })

	require.Equal(t, "owner-repo", entry.ID)
	require.Equal(t, "owner/repo", entry.DisplayName)
	require.Equal(t, "origin", entry.Remote)
	require.NotNil(t, entry.Engine)
	require.NotNil(t, entry.Broadcaster)
	require.NotEmpty(t, entry.RepoRoot)
}
