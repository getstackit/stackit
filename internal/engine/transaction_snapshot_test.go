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

// txEngine is the slice of the engine implementation these tests drive.
type txEngine interface {
	engine.Engine
	BeginTx(string) *engine.MetadataTx
	Metadata() *git.MetadataStore
}

func newTxEngine(t *testing.T, r git.Runner, dir string) txEngine {
	t.Helper()
	e, err := engine.NewEngine(engine.Options{RepoRoot: dir, Trunk: "main", Git: r})
	require.NoError(t, err)
	return e.(txEngine)
}

// metadataTier selects which metadata namespace a table case exercises.
type metadataTier int

const (
	sharedTier metadataTier = iota
	localTier
)

func (tier metadataTier) String() string {
	if tier == localTier {
		return "local"
	}
	return "shared"
}

// tierBranch is the branch the tier helpers operate on.
const tierBranch = "feature"

// stageTier reads and stages tierBranch in the chosen tier.
func stageTier(t *testing.T, tx *engine.MetadataTx, tier metadataTier) {
	t.Helper()
	if tier == localTier {
		meta, err := tx.ReadLocalMetadata(t.Context(), tierBranch).One()
		require.NoError(t, err)
		meta.Frozen = !meta.Frozen
		require.NoError(t, tx.UpdateLocalMeta(tierBranch, meta))
		return
	}
	meta, err := tx.ReadMetadata(t.Context(), tierBranch).One()
	require.NoError(t, err)
	require.NoError(t, tx.UpdateMeta(tierBranch, meta.WithLockReason(git.LockReasonUser)))
}

// writeTier writes a distinguishable tierBranch record directly through a store.
func writeTier(store *git.MetadataStore, tier metadataTier, marker int64) error {
	if tier == localTier {
		return store.WriteLocalMetadata(tierBranch, &git.LocalMeta{NavigationCommentID: &marker})
	}
	scope := fmt.Sprint(marker)
	return store.WriteMetadata(tierBranch, git.NewMeta().WithScope(&scope))
}

// readTierMarker reads the marker writeTier stored, bypassing any cache.
func readTierMarker(t *testing.T, r git.Runner, tier metadataTier) string {
	t.Helper()
	fresh := git.NewMetadataStore(r)
	if tier == localTier {
		meta, err := fresh.ReadLocalMetadata(t.Context(), tierBranch).One()
		require.NoError(t, err)
		require.NotNil(t, meta.NavigationCommentID)
		return fmt.Sprint(*meta.NavigationCommentID)
	}
	meta, err := fresh.ReadMetadata(t.Context(), tierBranch).One()
	require.NoError(t, err)
	require.NotNil(t, meta.GetScope())
	return *meta.GetScope()
}

func TestMetadataTransactionStagingWithoutReadErrors(t *testing.T) {
	t.Parallel()
	scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)
	e := newTxEngine(t, git.NewRunnerWithPath(scene.Dir, nil), scene.Dir)
	// The records exist on disk; only this transaction's read is missing.
	require.NoError(t, writeTier(e.Metadata(), sharedTier, 1))
	require.NoError(t, writeTier(e.Metadata(), localTier, 1))

	tx := e.BeginTx("stage without reading")
	const want = "metadata for feature must be read before staging"
	require.EqualError(t, tx.UpdateMeta("feature", git.NewMeta()), want)
	require.EqualError(t, tx.UpdateLocalMeta("feature", &git.LocalMeta{}), want)
	require.EqualError(t, tx.DeleteMeta("feature"), want)
	require.EqualError(t, tx.DeleteLocalMeta("feature"), want)

	// Preparing one tier does not prepare the other.
	require.Empty(t, tx.ReadMetadata(t.Context(), "feature").Failures())
	require.NoError(t, tx.UpdateMeta("feature", git.NewMeta()))
	require.NoError(t, tx.DeleteMeta("feature"))
	require.EqualError(t, tx.UpdateLocalMeta("feature", &git.LocalMeta{}), want)
	require.EqualError(t, tx.DeleteLocalMeta("feature"), want)
}

// A transaction pins the version of its first read. A re-read that observes a
// different version must fail rather than adopt it, or the commit would carry
// a newer expectation than the content the caller staged from.
func TestMetadataTransactionRereadAfterExternalChangeErrors(t *testing.T) {
	t.Parallel()
	for _, tier := range []metadataTier{sharedTier, localTier} {
		t.Run(tier.String(), func(t *testing.T) {
			t.Parallel()
			scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)
			r := git.NewRunnerWithPath(scene.Dir, nil)
			e := newTxEngine(t, r, scene.Dir)
			require.NoError(t, writeTier(e.Metadata(), tier, 1))

			read := func(tx *engine.MetadataTx) error {
				if tier == localTier {
					_, err := tx.ReadLocalMetadata(t.Context(), "feature").One()
					return err
				}
				_, err := tx.ReadMetadata(t.Context(), "feature").One()
				return err
			}
			tx := e.BeginTx("reread")
			require.NoError(t, read(tx))

			// Another store on the same runner, so the engine's store sees the
			// ref generation move and rereads the ref instead of its cache.
			require.NoError(t, writeTier(git.NewMetadataStore(r), tier, 2))

			require.EqualError(t, read(tx), "metadata version changed after preparing feature")
		})
	}
}

// inProcessRefWrite is a way this process rewrites a metadata ref through
// UpdateRefs, behind the store's back.
type inProcessRefWrite struct {
	name  string
	tier  metadataTier
	write func(*testing.T, txEngine)
}

func inProcessRefWrites() []inProcessRefWrite {
	commitUpdate := func(tier metadataTier) func(*testing.T, txEngine) {
		return func(t *testing.T, e txEngine) {
			t.Helper()
			tx := e.BeginTx("tx update")
			stageTier(t, tx, tier)
			require.NoError(t, tx.Commit(t.Context()))
		}
	}
	commitDelete := func(tier metadataTier) func(*testing.T, txEngine) {
		return func(t *testing.T, e txEngine) {
			t.Helper()
			tx := e.BeginTx("tx delete")
			if tier == localTier {
				require.Empty(t, tx.ReadLocalMetadata(t.Context(), tierBranch).Failures())
				require.NoError(t, tx.DeleteLocalMeta(tierBranch))
			} else {
				require.Empty(t, tx.ReadMetadata(t.Context(), tierBranch).Failures())
				require.NoError(t, tx.DeleteMeta(tierBranch))
			}
			require.NoError(t, tx.Commit(t.Context()))
		}
	}
	return []inProcessRefWrite{
		{name: "shared tx update", tier: sharedTier, write: commitUpdate(sharedTier)},
		{name: "local tx update", tier: localTier, write: commitUpdate(localTier)},
		{name: "shared tx delete", tier: sharedTier, write: commitDelete(sharedTier)},
		{name: "local tx delete", tier: localTier, write: commitDelete(localTier)},
		{name: "local MarkBranchesForPRBodyUpdate", tier: localTier, write: func(t *testing.T, e txEngine) {
			t.Helper()
			require.NoError(t, e.MarkBranchesForPRBodyUpdate(t.Context(), []string{tierBranch}))
		}},
	}
}

// After this process rewrites a metadata ref outside the store, the store's
// earlier expectation predates that write. A direct write that follows with no
// read in between must not compare against it and fail as "another process
// changed it".
func TestMetadataDirectWriteAfterInProcessRefUpdateSucceeds(t *testing.T) {
	t.Parallel()
	for _, tc := range inProcessRefWrites() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)
			r := git.NewRunnerWithPath(scene.Dir, nil)
			e := newTxEngine(t, r, scene.Dir)
			// The direct write records an expectation in the store, exactly
			// as a read would.
			require.NoError(t, writeTier(e.Metadata(), tc.tier, 1))

			tc.write(t, e)

			require.NoError(t, writeTier(e.Metadata(), tc.tier, 2))
			require.Equal(t, "2", readTierMarker(t, r, tc.tier))
		})
	}
}

// The same sequence with another process writing in between. The store must
// know what this process's own write left on the ref — not merely distrust its
// older expectation — or the blind write is unconditional and silently
// discards the other process's change.
func TestMetadataBlindWriteAfterInProcessRefUpdateCatchesOtherProcess(t *testing.T) {
	t.Parallel()
	for _, tc := range inProcessRefWrites() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)
			r := git.NewRunnerWithPath(scene.Dir, nil)
			e := newTxEngine(t, r, scene.Dir)
			// A separate runner stands in for another process: its writes do
			// not move this engine's ref generations.
			otherProcess := git.NewMetadataStore(git.NewRunnerWithPath(scene.Dir, nil))
			require.NoError(t, writeTier(e.Metadata(), tc.tier, 1))

			tc.write(t, e)
			require.NoError(t, writeTier(otherProcess, tc.tier, 2))

			require.ErrorContains(t, writeTier(e.Metadata(), tc.tier, 3), "another process changed it")
			require.Equal(t, "2", readTierMarker(t, r, tc.tier), "the other process's write must survive")
		})
	}
}

// A rejected commit must not leave its stale versions cached. Otherwise the
// next transaction reads the same versions back from cache and fails the same
// way until something rebuilds the engine — a wedge in the long-lived server.
func TestMetadataTransactionRejectedCommitDropsStaleCache(t *testing.T) {
	t.Parallel()
	for _, tier := range []metadataTier{sharedTier, localTier} {
		for _, followUp := range []string{"transaction", "direct write"} {
			t.Run(tier.String()+"/"+followUp, func(t *testing.T) {
				t.Parallel()
				scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)
				r := git.NewRunnerWithPath(scene.Dir, nil)
				e := newTxEngine(t, r, scene.Dir)
				// A separate runner stands in for another process: its writes
				// do not move this engine's ref generations.
				otherProcess := git.NewMetadataStore(git.NewRunnerWithPath(scene.Dir, nil))
				require.NoError(t, writeTier(e.Metadata(), tier, 1))

				rejected := e.BeginTx("rejected")
				stageTier(t, rejected, tier)
				require.NoError(t, writeTier(otherProcess, tier, 2))
				err := rejected.Commit(t.Context())
				require.Error(t, err)
				require.True(t, engine.IsConcurrentModificationError(err), "got: %v", err)

				// No rebuild and no ClearMetadataCache: the follow-up must
				// succeed on the strength of the commit's invalidation alone.
				if followUp == "direct write" {
					// The rejection dropped the expectation, so this blind
					// write is unconditional by the documented unknown-
					// expectation rule and replaces the other process's
					// value. That is the accepted cost of not wedging;
					// callers that care read first.
					require.NoError(t, writeTier(e.Metadata(), tier, 3))
					require.Equal(t, "3", readTierMarker(t, r, tier))
					return
				}
				retry := e.BeginTx("retry")
				stageTier(t, retry, tier)
				require.NoError(t, retry.Commit(t.Context()))
				// The retry built on the other process's write, not over it.
				require.Equal(t, "2", readTierMarker(t, r, tier))
			})
		}
	}
}
