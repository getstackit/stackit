package git_test

import (
	"fmt"
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

// A ref rewritten through the backend by this process (the path transaction
// commits take) moves its generation. The store's recorded SHA then predates
// this process's own write; a following direct write must treat it as unknown
// rather than reject the write as another process's change. A write through a
// different runner does not move the generation and must still be rejected.
func TestMetadataStoreDistrustsExpectationAfterInProcessRefUpdate(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		refName string
		write   func(*git.MetadataStore) error
	}{
		{
			name:    "shared",
			refName: git.MetadataRefName("feature"),
			write:   func(store *git.MetadataStore) error { return store.WriteMetadata("feature", git.NewMeta()) },
		},
		{
			name:    "local",
			refName: git.LocalMetadataRefName("feature"),
			write:   func(store *git.MetadataStore) error { return store.WriteLocalMetadata("feature", &git.LocalMeta{}) },
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)
			r := git.NewRunnerWithPath(scene.Dir, nil)
			store := git.NewMetadataStore(r)
			require.NoError(t, tc.write(store))

			sha, err := git.One(r.CreateBlobs(t.Context(), `{"frozen":true}`))
			require.NoError(t, err)
			require.NoError(t, r.UpdateRefs(t.Context(), []git.RefUpdate{{RefName: tc.refName, NewSHA: sha}}, ""))
			require.NoError(t, tc.write(store), "this process's own ref update is not a concurrent change")

			other := git.NewRunnerWithPath(scene.Dir, nil)
			require.NoError(t, other.UpdateRefs(t.Context(), []git.RefUpdate{{RefName: tc.refName, NewSHA: sha}}, ""))
			require.ErrorContains(t, tc.write(store), "another process changed it")
		})
	}
}

// RecordCommitted makes a ref this process wrote outside the store the next
// expectation, so a blind write after it still catches another process. A
// recorded delete requires the ref to stay missing.
func TestMetadataStoreRecordCommitted(t *testing.T) {
	t.Parallel()
	type tierCase struct {
		name    string
		tier    git.MetadataTier
		refName string
		write   func(*git.MetadataStore) error
	}
	tiers := []tierCase{
		{
			name:    "shared",
			tier:    git.MetadataTierShared,
			refName: git.MetadataRefName("feature"),
			write:   func(store *git.MetadataStore) error { return store.WriteMetadata("feature", git.NewMeta()) },
		},
		{
			name:    "local",
			tier:    git.MetadataTierLocal,
			refName: git.LocalMetadataRefName("feature"),
			write:   func(store *git.MetadataStore) error { return store.WriteLocalMetadata("feature", &git.LocalMeta{}) },
		},
	}
	for _, tc := range tiers {
		for _, deleted := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/deleted=%t", tc.name, deleted), func(t *testing.T) {
				t.Parallel()
				scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)
				r := git.NewRunnerWithPath(scene.Dir, nil)
				store := git.NewMetadataStore(r)
				require.NoError(t, tc.write(store))

				// This process rewrites the ref behind the store's back.
				committed := git.CommittedMetadata{Branch: "feature"}
				if deleted {
					require.NoError(t, r.DeleteRefs(t.Context(), tc.refName))
				} else {
					sha, err := git.One(r.CreateBlobs(t.Context(), `{"frozen":true}`))
					require.NoError(t, err)
					require.NoError(t, r.UpdateRefs(t.Context(), []git.RefUpdate{{RefName: tc.refName, NewSHA: sha}}, ""))
					committed.SHA = sha
					if tc.tier == git.MetadataTierShared {
						committed.Meta = git.NewMeta()
					}
				}
				store.RecordCommitted(tc.tier, committed)

				other := git.NewRunnerWithPath(scene.Dir, nil)
				theirs, err := git.One(other.CreateBlobs(t.Context(), `{"theirs":true}`))
				require.NoError(t, err)
				require.NoError(t, other.UpdateRefs(t.Context(), []git.RefUpdate{{RefName: tc.refName, NewSHA: theirs}}, ""))

				require.ErrorContains(t, tc.write(store), "another process changed it")
				onDisk, err := other.ReadRevisions(t.Context(), tc.refName).One()
				require.NoError(t, err)
				require.Equal(t, theirs, onDisk, "the other process's write must survive")
			})
		}
	}
}
