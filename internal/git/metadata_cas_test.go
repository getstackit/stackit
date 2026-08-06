package git_test

import (
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
		second := git.NewRunnerWithPath(scene.Repo.Dir, nil)

		main := "main"
		require.NoError(t, first.WriteMetadata("feature", git.NewMeta().WithParentBranchName(&main)))

		// Both read the same starting point.
		fromFirst, err := first.ReadMetadata("feature")
		require.NoError(t, err)
		fromSecond, err := second.ReadMetadata("feature")
		require.NoError(t, err)
		require.Equal(t, "main", *fromSecond.GetParentBranchName())

		// First writes.
		firstParent := "first-parent"
		require.NoError(t, first.WriteMetadata("feature", fromFirst.WithParentBranchName(&firstParent)))

		// Second writes based on what it read before that.
		secondParent := "second-parent"
		err = second.WriteMetadata("feature", fromSecond.WithParentBranchName(&secondParent))
		require.Error(t, err, "a write based on superseded state must not silently win")
		require.Contains(t, err.Error(), "another process changed it")

		// The first write survives.
		latest, err := first.ReadMetadata("feature")
		require.NoError(t, err)
		require.Equal(t, "first-parent", *latest.GetParentBranchName())
	})

	t.Run("write after re-reading succeeds", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)
		first := git.NewRunnerWithPath(scene.Repo.Dir, nil)
		second := git.NewRunnerWithPath(scene.Repo.Dir, nil)

		main := "main"
		require.NoError(t, first.WriteMetadata("feature", git.NewMeta().WithParentBranchName(&main)))

		fromFirst, err := first.ReadMetadata("feature")
		require.NoError(t, err)
		firstParent := "first-parent"
		require.NoError(t, first.WriteMetadata("feature", fromFirst.WithParentBranchName(&firstParent)))

		// Re-reading picks up the new blob, so the follow-up write is based on
		// current state and is allowed.
		fresh, err := second.ReadMetadata("feature")
		require.NoError(t, err)
		require.Equal(t, "first-parent", *fresh.GetParentBranchName())
		secondParent := "second-parent"
		require.NoError(t, second.WriteMetadata("feature", fresh.WithParentBranchName(&secondParent)))

		latest, err := second.ReadMetadata("feature")
		require.NoError(t, err)
		require.Equal(t, "second-parent", *latest.GetParentBranchName())
	})

	t.Run("first write of a branch needs no expectation", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)
		runner := git.NewRunnerWithPath(scene.Repo.Dir, nil)

		// Never read: tracking a new branch must not require one.
		main := "main"
		require.NoError(t, runner.WriteMetadata("brand-new", git.NewMeta().WithParentBranchName(&main)))

		stored, err := runner.ReadMetadata("brand-new")
		require.NoError(t, err)
		require.Equal(t, "main", *stored.GetParentBranchName())
	})
}
