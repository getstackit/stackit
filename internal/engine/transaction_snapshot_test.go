package engine_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/testhelpers"
)

type metadataCountingRunner struct {
	git.Runner
	reads, revisions, blobs, updates int
}

func (r *metadataCountingRunner) ReadObjects(ctx context.Context, refs ...string) (map[string]git.BatchObject, error) {
	r.reads++
	return r.Runner.ReadObjects(ctx, refs...)
}

func (r *metadataCountingRunner) ReadRevisions(ctx context.Context, refs ...string) git.ReadResults[string] {
	r.revisions++
	return r.Runner.ReadRevisions(ctx, refs...)
}

func (r *metadataCountingRunner) CreateBlobs(ctx context.Context, values ...string) ([]string, error) {
	r.blobs++
	return r.Runner.CreateBlobs(ctx, values...)
}

func (r *metadataCountingRunner) UpdateRefs(ctx context.Context, updates []git.RefUpdate, message string) error {
	r.updates++
	return r.Runner.UpdateRefs(ctx, updates, message)
}

func TestMetadataTransactionBatchesBothTiers(t *testing.T) {
	t.Parallel()
	for _, count := range []int{1, 30} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			t.Parallel()
			scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)
			r := &metadataCountingRunner{Runner: git.NewRunnerWithPath(scene.Dir, nil)}
			e, err := engine.NewEngine(engine.Options{RepoRoot: scene.Dir, Trunk: "main", Git: r, LoadMode: engine.LoadModeBranchesOnly})
			require.NoError(t, err)
			names := make([]string, count)
			for i := range names {
				names[i] = fmt.Sprintf("feature-%d", i)
			}
			tx := e.(interface {
				BeginTx(string) *engine.MetadataTx
			}).BeginTx("mixed metadata")
			shared := tx.ReadMetadata(t.Context(), names...)
			local := tx.ReadLocalMetadata(t.Context(), names...)
			require.Empty(t, shared.Failures())
			require.Empty(t, local.Failures())
			require.Equal(t, 2, r.reads)
			for _, name := range names {
				require.NoError(t, tx.UpdateMeta(name, shared.Values()[name].WithLockReason(git.LockReasonUser)))
				local.Values()[name].Frozen = true
				require.NoError(t, tx.UpdateLocalMeta(name, local.Values()[name]))
			}
			require.Equal(t, 2, r.reads, "staging must not perform reads")
			require.Zero(t, r.revisions, "staging must not reread ref SHAs")
			require.Zero(t, r.blobs)
			require.Zero(t, r.updates)
			require.NoError(t, tx.Commit(t.Context()))
			require.Equal(t, 1, r.blobs, "both tiers must share one blob write")
			require.Equal(t, 1, r.updates)
			fresh := git.NewMetadataStore(r.Runner)
			for _, name := range names {
				meta, err := fresh.ReadMetadata(t.Context(), name).One()
				require.NoError(t, err)
				require.Equal(t, git.LockReasonUser, meta.GetLockReason())
				local, err := fresh.ReadLocalMetadata(t.Context(), name).One()
				require.NoError(t, err)
				require.True(t, local.Frozen)
			}
		})
	}
}

func TestMetadataTransactionProtectsReadBeforeStaging(t *testing.T) {
	t.Parallel()
	for _, local := range []bool{false, true} {
		for _, initial := range []string{"missing", "empty", "existing"} {
			t.Run(fmt.Sprintf("local=%t/initial=%s", local, initial), func(t *testing.T) {
				t.Parallel()
				scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)
				otherRunner := git.NewRunnerWithPath(scene.Dir, nil)
				other := git.NewMetadataStore(otherRunner)
				if initial == "empty" {
					sha, err := git.One(otherRunner.CreateBlobs(t.Context(), ""))
					require.NoError(t, err)
					ref := git.MetadataRefName("feature")
					if local {
						ref = git.LocalMetadataRefName("feature")
					}
					require.NoError(t, otherRunner.UpdateRefs(t.Context(), []git.RefUpdate{{RefName: ref, NewSHA: sha}}, ""))
				}
				if initial == "existing" {
					if local {
						require.NoError(t, other.WriteLocalMetadata("feature", &git.LocalMeta{}))
					} else {
						require.NoError(t, other.WriteMetadata("feature", git.NewMeta()))
					}
				}
				e, err := engine.NewEngine(engine.Options{RepoRoot: scene.Dir, Trunk: "main"})
				require.NoError(t, err)
				tx := e.(interface {
					BeginTx(string) *engine.MetadataTx
				}).BeginTx("protect the read version")
				if local {
					meta, err := tx.ReadLocalMetadata(t.Context(), "feature").One()
					require.NoError(t, err)
					require.NoError(t, other.WriteLocalMetadata("feature", &git.LocalMeta{NeedsPRBodyUpdate: true}))
					meta.Frozen = true
					require.NoError(t, tx.UpdateLocalMeta("feature", meta))
				} else {
					meta, err := tx.ReadMetadata(t.Context(), "feature").One()
					require.NoError(t, err)
					scope := "external"
					require.NoError(t, other.WriteMetadata("feature", git.NewMeta().WithScope(&scope)))
					require.NoError(t, tx.UpdateMeta("feature", meta.WithLockReason(git.LockReasonUser)))
				}
				require.Error(t, tx.Commit(t.Context()), "a change after reading must be rejected even before staging")
			})
		}
	}
}

func TestMetadataTransactionRequiresPreparation(t *testing.T) {
	t.Parallel()
	scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)
	e, err := engine.NewEngine(engine.Options{RepoRoot: scene.Dir, Trunk: "main"})
	require.NoError(t, err)
	tx := e.(interface {
		BeginTx(string) *engine.MetadataTx
	}).BeginTx("no implicit reads")
	require.ErrorContains(t, tx.UpdateMeta("unknown", git.NewMeta()), "must be read")
	require.ErrorContains(t, tx.UpdateLocalMeta("unknown", &git.LocalMeta{}), "must be read")
	require.ErrorContains(t, tx.DeleteMeta("unknown"), "must be read")
	require.ErrorContains(t, tx.DeleteLocalMeta("unknown"), "must be read")
}
