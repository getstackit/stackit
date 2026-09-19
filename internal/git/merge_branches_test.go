package git_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/testhelpers"
)

func TestMergeBranchesHonorsFastForwardOnly(t *testing.T) {
	t.Parallel()
	scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)
	require.NoError(t, scene.Repo.CreateAndCheckoutBranch("feature"))
	require.NoError(t, scene.Repo.CreateChangeAndCommit("feature", "feature"))
	require.NoError(t, scene.Repo.CheckoutBranch("main"))
	require.NoError(t, scene.Repo.CreateChangeAndCommit("main", "main"))

	runner := git.NewRunnerWithPath(scene.Dir, nil)
	before, err := runner.ReadRevisions(t.Context(), "HEAD").One()
	require.NoError(t, err)
	require.NoError(t, runner.MergeBranches(t.Context(), nil, git.MergeOptions{}))
	require.Error(t, runner.MergeBranches(t.Context(), []string{"feature"}, git.MergeOptions{FFOnly: true}))
	after, err := runner.ReadRevisions(t.Context(), "HEAD").One()
	require.NoError(t, err)
	require.Equal(t, before, after)
	require.False(t, runner.IsMergeInProgress(t.Context()))

	require.NoError(t, runner.MergeBranches(t.Context(), []string{"feature"}, git.MergeOptions{NoEdit: true}))
	merged, err := runner.IsAncestor(t.Context(), "feature", "HEAD")
	require.NoError(t, err)
	require.True(t, merged)
}
