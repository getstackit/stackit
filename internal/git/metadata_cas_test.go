package git_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/testhelpers"
)

// TestMetadataWriteCompareAndSwap covers two stackit processes writing the same
// branch's metadata.
//
// Both runners read the same state, then both write. The writes used to be
// unconditional `update-ref`, so the second silently discarded whatever the
// first recorded — a parent change, a PR number, a scope. Each runner now
// remembers the blob it read and requires the ref to still hold it, so the
// stale write fails loudly instead.
func TestMetadataWriteCompareAndSwap(t *testing.T) {
	t.Parallel()

	t.Run("stale write is rejected", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)

		// Two runners stand in for two processes against the same repo.
		first := git.NewRunnerWithPath(scene.Repo.Dir, nil)
		firstMetadata := git.NewMetadataStore(first)
		second := git.NewRunnerWithPath(scene.Repo.Dir, nil)
		secondMetadata := git.NewMetadataStore(second)

		main := "main"
		require.NoError(t, firstMetadata.WriteMetadata("feature", git.NewMeta().WithParentBranchName(&main)))

		// Both read the same starting point.
		fromFirst, err := firstMetadata.ReadMetadata(context.Background(), "feature").One()
		require.NoError(t, err)
		fromSecond, err := secondMetadata.ReadMetadata(context.Background(), "feature").One()
		require.NoError(t, err)
		require.Equal(t, "main", *fromSecond.GetParentBranchName())

		// First writes.
		firstParent := "first-parent"
		require.NoError(t, firstMetadata.WriteMetadata("feature", fromFirst.WithParentBranchName(&firstParent)))

		// Second writes based on what it read before that.
		secondParent := "second-parent"
		err = secondMetadata.WriteMetadata("feature", fromSecond.WithParentBranchName(&secondParent))
		require.Error(t, err, "a write based on superseded state must not silently win")
		require.Contains(t, err.Error(), "another process changed it")

		// The first write survives.
		latest, err := firstMetadata.ReadMetadata(context.Background(), "feature").One()
		require.NoError(t, err)
		require.Equal(t, "first-parent", *latest.GetParentBranchName())
	})

	// The guard is worthless if it only covers the read path almost nothing
	// uses. Engine graph loads go through ReadMetadata, which warmed the
	// cache without a SHA — so every command that read a stack and then wrote
	// to it had no expectation to compare against and fell back to a blind
	// update-ref. This is the same scenario as "stale write is rejected",
	// seeded the way real commands seed it.
	t.Run("stale write is rejected after a batch read", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)

		first := git.NewRunnerWithPath(scene.Repo.Dir, nil)
		firstMetadata := git.NewMetadataStore(first)
		second := git.NewRunnerWithPath(scene.Repo.Dir, nil)
		secondMetadata := git.NewMetadataStore(second)

		main := "main"
		require.NoError(t, firstMetadata.WriteMetadata("feature", git.NewMeta().WithParentBranchName(&main)))

		firstBatch, errs := firstMetadata.ReadMetadata(context.Background(), []string{"feature"}...).Split()
		require.Empty(t, errs)
		secondBatch, errs := secondMetadata.ReadMetadata(context.Background(), []string{"feature"}...).Split()
		require.Empty(t, errs)

		firstParent := "first-parent"
		require.NoError(t, firstMetadata.WriteMetadata("feature", firstBatch["feature"].WithParentBranchName(&firstParent)))

		secondParent := "second-parent"
		err := secondMetadata.WriteMetadata("feature", secondBatch["feature"].WithParentBranchName(&secondParent))
		require.Error(t, err, "a write based on superseded state must not silently win")
		require.Contains(t, err.Error(), "another process changed it")

		latest, err := firstMetadata.ReadMetadata(context.Background(), "feature").One()
		require.NoError(t, err)
		require.Equal(t, "first-parent", *latest.GetParentBranchName())
	})

	// A failed compare-and-swap leaves the process holding an expectation that
	// is known-stale. Keeping it meant a re-read answered from cache, recomputed
	// the same expectation, and failed identically forever — invisible in the
	// short-lived CLI, a wedge in the long-lived server.
	t.Run("a rejected write does not wedge later writes", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)

		first := git.NewRunnerWithPath(scene.Repo.Dir, nil)
		firstMetadata := git.NewMetadataStore(first)
		second := git.NewRunnerWithPath(scene.Repo.Dir, nil)
		secondMetadata := git.NewMetadataStore(second)

		main := "main"
		require.NoError(t, firstMetadata.WriteMetadata("feature", git.NewMeta().WithParentBranchName(&main)))

		fromSecond, err := secondMetadata.ReadMetadata(context.Background(), "feature").One()
		require.NoError(t, err)

		fromFirst, err := firstMetadata.ReadMetadata(context.Background(), "feature").One()
		require.NoError(t, err)
		firstParent := "first-parent"
		require.NoError(t, firstMetadata.WriteMetadata("feature", fromFirst.WithParentBranchName(&firstParent)))

		secondParent := "second-parent"
		require.Error(t, secondMetadata.WriteMetadata("feature", fromSecond.WithParentBranchName(&secondParent)))

		// Re-reading in the same process must see the winner, not the stale
		// cached copy, and the retry must then be allowed through.
		fresh, err := secondMetadata.ReadMetadata(context.Background(), "feature").One()
		require.NoError(t, err)
		require.Equal(t, "first-parent", *fresh.GetParentBranchName())
		require.NoError(t, secondMetadata.WriteMetadata("feature", fresh.WithParentBranchName(&secondParent)))

		// Read through a third runner: first still holds its own cached copy
		// from before second's write, which is ordinary cache staleness and not
		// what this case is about.
		third := git.NewRunnerWithPath(scene.Repo.Dir, nil)
		thirdMetadata := git.NewMetadataStore(third)
		latest, err := thirdMetadata.ReadMetadata(context.Background(), "feature").One()
		require.NoError(t, err)
		require.Equal(t, "second-parent", *latest.GetParentBranchName())
	})

	t.Run("write after re-reading succeeds", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)
		first := git.NewRunnerWithPath(scene.Repo.Dir, nil)
		firstMetadata := git.NewMetadataStore(first)
		second := git.NewRunnerWithPath(scene.Repo.Dir, nil)
		secondMetadata := git.NewMetadataStore(second)

		main := "main"
		require.NoError(t, firstMetadata.WriteMetadata("feature", git.NewMeta().WithParentBranchName(&main)))

		fromFirst, err := firstMetadata.ReadMetadata(context.Background(), "feature").One()
		require.NoError(t, err)
		firstParent := "first-parent"
		require.NoError(t, firstMetadata.WriteMetadata("feature", fromFirst.WithParentBranchName(&firstParent)))

		// Re-reading picks up the new blob, so the follow-up write is based on
		// current state and is allowed.
		fresh, err := secondMetadata.ReadMetadata(context.Background(), "feature").One()
		require.NoError(t, err)
		require.Equal(t, "first-parent", *fresh.GetParentBranchName())
		secondParent := "second-parent"
		require.NoError(t, secondMetadata.WriteMetadata("feature", fresh.WithParentBranchName(&secondParent)))

		latest, err := secondMetadata.ReadMetadata(context.Background(), "feature").One()
		require.NoError(t, err)
		require.Equal(t, "second-parent", *latest.GetParentBranchName())
	})

	t.Run("first write of a branch needs no expectation", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)
		runner := git.NewRunnerWithPath(scene.Repo.Dir, nil)
		runnerMetadata := git.NewMetadataStore(runner)

		// Never read: tracking a new branch must not require one.
		main := "main"
		require.NoError(t, runnerMetadata.WriteMetadata("brand-new", git.NewMeta().WithParentBranchName(&main)))

		stored, err := runnerMetadata.ReadMetadata(context.Background(), "brand-new").One()
		require.NoError(t, err)
		require.Equal(t, "main", *stored.GetParentBranchName())
	})
}
