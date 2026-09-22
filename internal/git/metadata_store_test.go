package git_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/testhelpers"
)

func TestMetadataStoreKeepsValuesAndVersionsTogether(t *testing.T) {
	t.Parallel()
	scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)
	r := git.NewRunnerWithPath(scene.Dir, nil)
	store := git.NewMetadataStore(r)
	shas, err := r.CreateBlobs(t.Context(), "", "{invalid")
	require.NoError(t, err)
	require.NoError(t, r.UpdateRefs(t.Context(), []git.RefUpdate{
		{RefName: git.MetadataRefName("empty"), NewSHA: shas[0]},
		{RefName: git.MetadataRefName("corrupt"), NewSHA: shas[1]},
	}, ""))
	read := store.ReadMetadata(t.Context(), "missing", "empty", "corrupt")
	missing, err := read.Record("missing")
	require.NoError(t, err)
	require.NotNil(t, missing.Value)
	require.Empty(t, missing.SHA)
	empty, err := read.Record("empty")
	require.NoError(t, err)
	require.Equal(t, shas[0], empty.SHA)
	corrupt, err := read.Record("corrupt")
	require.Error(t, err)
	require.Equal(t, shas[1], corrupt.SHA, "even an unreadable record retains the version needed for deletion")
	_, err = read.Record("unrequested")
	require.Error(t, err)
	// Cached empty content must retain its nonempty version too.
	cached, err := store.ReadMetadata(t.Context(), "empty").Record("empty")
	require.NoError(t, err)
	require.Equal(t, empty, cached)
}

func TestMetadataStoreInvalidatesOnlyChangedRefs(t *testing.T) {
	t.Parallel()
	scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)
	r := git.NewRunnerWithPath(scene.Dir, nil)
	store := git.NewMetadataStore(r)
	require.NoError(t, store.WriteMetadata("a", git.NewMeta()))
	require.NoError(t, store.WriteMetadata("b", git.NewMeta()))
	before := store.ReadMetadata(t.Context(), "a", "b")
	require.Empty(t, before.Failures())
	require.NoError(t, r.DeleteRefs(t.Context(), git.MetadataRefName("b")))
	after := store.ReadMetadata(t.Context(), "a", "b")
	require.Empty(t, after.Failures())
	require.Equal(t, before.Versions["a"], after.Versions["a"])
	require.Empty(t, after.Versions["b"])

	// An unrelated write must not discard a's optimistic-locking expectation.
	other := git.NewMetadataStore(git.NewRunnerWithPath(scene.Dir, nil))
	require.NoError(t, other.WriteMetadata("a", git.NewMeta().WithLockReason(git.LockReasonUser)))
	require.NoError(t, store.WriteMetadata("b", git.NewMeta()))
	require.Error(t, store.WriteMetadata("a", before.Values()["a"]))
}
