package git_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/testhelpers"
)

func TestMetadataReadsPreservePartialFailures(t *testing.T) {
	t.Parallel()
	scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)
	runner := git.NewRunnerWithPath(scene.Dir, nil)
	runnerMetadata := git.NewMetadataStore(runner)
	require.NoError(t, runnerMetadata.WriteMetadata("valid", git.NewMeta()))
	require.NoError(t, runnerMetadata.WriteLocalMetadata("valid", &git.LocalMeta{Frozen: true}))
	corrupt, err := git.One(runner.CreateBlobs(context.Background(), "{bad JSON"))
	require.NoError(t, err)
	for _, prefix := range []string{git.MetadataRefPrefix, git.LocalMetadataRefPrefix} {
		require.NoError(t, runner.UpdateRefs(context.Background(), []git.RefUpdate{{RefName: prefix + "corrupt", NewSHA: corrupt}}, ""))
	}
	shared := runnerMetadata.ReadMetadata(t.Context(), "valid", "missing", "corrupt")
	require.Len(t, shared.Values, 2)
	require.ErrorContains(t, shared.Errors["corrupt"], "unmarshal")
	local := runnerMetadata.ReadLocalMetadata(t.Context(), "valid", "missing", "corrupt")
	require.Len(t, local.Values, 2)
	require.True(t, local.Values["valid"].Frozen)
	require.False(t, local.Values["missing"].Frozen)
	require.ErrorContains(t, local.Errors["corrupt"], "unmarshal")
	_, err = runnerMetadata.ReadLocalMetadata(t.Context(), "corrupt").One()
	require.ErrorContains(t, err, "unmarshal")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = runnerMetadata.ReadMetadata(ctx, "valid").One()
	require.ErrorIs(t, err, context.Canceled)
	_, err = runnerMetadata.ReadLocalMetadata(ctx, "valid").One()
	require.ErrorIs(t, err, context.Canceled)
}
