package git_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/testhelpers"
)

func TestBatchReadMetadataForBranches(t *testing.T) {
	t.Parallel()

	scene := testhelpers.NewSceneParallel(t, nil)
	runner := git.NewRunnerWithPath(scene.Dir, nil)

	parent := "main"
	parentRev := testRevisionABC
	scope := "api"
	require.NoError(t, runner.WriteMetadata("feature-a", git.NewMetaFrom(git.MetaFields{
		ParentBranchName:     &parent,
		ParentBranchRevision: &parentRev,
		Scope:                &scope,
	})))

	metas, errs := runner.BatchReadMetadataForBranches([]string{"feature-a", "missing-branch"})
	require.Empty(t, errs)
	require.Contains(t, metas, "feature-a")
	require.Contains(t, metas, "missing-branch")
	require.Equal(t, "main", *metas["feature-a"].GetParentBranchName())
	require.Equal(t, testRevisionABC, *metas["feature-a"].GetParentBranchRevision())
	require.Equal(t, "api", *metas["feature-a"].GetScope())
	require.Nil(t, metas["missing-branch"].GetParentBranchName())
}

func TestBatchReadLocalMetadataForBranches(t *testing.T) {
	t.Parallel()

	scene := testhelpers.NewSceneParallel(t, nil)
	runner := git.NewRunnerWithPath(scene.Dir, nil)

	require.NoError(t, runner.WriteLocalMetadata("feature-a", &git.LocalMeta{
		Frozen:            true,
		NeedsPRBodyUpdate: true,
	}))

	metas := runner.BatchReadLocalMetadataForBranches([]string{"feature-a", "missing-branch"})
	require.Contains(t, metas, "feature-a")
	require.Contains(t, metas, "missing-branch")
	require.True(t, metas["feature-a"].Frozen)
	require.True(t, metas["feature-a"].NeedsPRBodyUpdate)
	require.False(t, metas["missing-branch"].Frozen)
	require.False(t, metas["missing-branch"].NeedsPRBodyUpdate)
}
