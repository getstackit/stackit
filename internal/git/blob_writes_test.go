package git_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/testhelpers"
)

func TestCreateBlobsChoosesFastPath(t *testing.T) {
	t.Parallel()
	scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)
	logger := &traceCaptureLogger{}
	runner := git.NewRunnerWithPath(scene.Dir, logger)
	for _, contents := range [][]string{nil, {"one"}, {"", "binary\x00\n", "binary\x00\n"}} {
		logger.calls = 0
		shas, err := runner.CreateBlobs(t.Context(), contents...)
		require.NoError(t, err)
		require.Len(t, shas, len(contents))
		if len(contents) == 0 {
			require.Zero(t, logger.calls)
		} else {
			require.Equal(t, 1, logger.calls)
		}
		for i, sha := range shas {
			got, err := runner.ReadBlob(sha)
			require.NoError(t, err)
			require.Equal(t, contents[i], got)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := git.One(runner.CreateBlobs(ctx, "canceled"))
	require.ErrorIs(t, err, context.Canceled)
	_, err = git.One([]string(nil), nil)
	require.Error(t, err)
}
