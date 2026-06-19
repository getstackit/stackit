package engine_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

// TestBatchBranchStats asserts the batched stats match the per-branch accessors
// they replace, so annotation builders can read from the batch instead of
// warming the engine-global caches.
func TestBatchBranchStats(t *testing.T) {
	t.Parallel()
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
	s.WithLinearStack3()

	branches := s.Engine.AllBranches()
	stats := s.Engine.BatchBranchStats(branches)

	for _, b := range branches {
		name := b.GetName()
		stat, ok := stats[name]
		require.True(t, ok, "missing stat for %s", name)

		// Short SHA matches GetRevision for every branch, including trunk.
		if rev, err := s.Engine.GetRevision(b); err == nil && len(rev) >= 7 {
			require.Equal(t, rev[:7], stat.ShortSHA, "ShortSHA for %s", name)
		}

		if b.IsTrunk() {
			continue
		}

		wantCount, err := s.Engine.GetCommitCount(b)
		require.NoError(t, err)
		require.Equal(t, wantCount, stat.CommitCount, "CommitCount for %s", name)

		wantAdded, wantDeleted, err := s.Engine.GetDiffStats(b)
		require.NoError(t, err)
		require.Equal(t, wantAdded, stat.LinesAdded, "LinesAdded for %s", name)
		require.Equal(t, wantDeleted, stat.LinesDeleted, "LinesDeleted for %s", name)
	}
}

// TestBatchChangedFileCountsUsesDivergenceBase verifies the file count is
// measured against the branch's divergence point (the same base as diff stats),
// not its parent's current tip — so it stays consistent when the parent advances
// past the branch without a restack.
func TestBatchChangedFileCountsUsesDivergenceBase(t *testing.T) {
	t.Parallel()
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
	s.WithLinearStack3() // main -> a -> b -> c

	b := engine.BranchesOf(s.Engine.GetBranch("b"))

	before := s.Engine.BatchChangedFileCounts(context.Background(), b)["b"]
	require.GreaterOrEqual(t, before, 1)

	// Advance b's parent (a) with a new commit touching a different file, without
	// restacking b, so a's current tip moves past b's divergence point.
	s.Checkout("a").CommitChange("a-advanced.txt", "advance a past b's divergence")

	after := s.Engine.BatchChangedFileCounts(context.Background(), b)["b"]
	require.Equal(t, before, after,
		"files-changed must use the divergence base, not the parent's current tip")
}

// TestPerConcernBatchReaders asserts each per-concern batch reader matches the
// single-branch accessor it batches, so consumers can compose the value maps.
func TestPerConcernBatchReaders(t *testing.T) {
	t.Parallel()
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
	s.WithLinearStack3()

	branches := s.Engine.AllBranches()
	diffs := s.Engine.BatchDiffStats(branches)
	commits := s.Engine.BatchCommits(branches, engine.CommitFormatReadable)
	divergence := s.Engine.BatchDivergencePoints(branches)

	for _, b := range branches {
		name := b.GetName()

		if b.IsTrunk() {
			continue
		}

		wantDiv, err := s.Engine.GetDivergencePoint(name)
		require.NoError(t, err)
		require.Equal(t, wantDiv, divergence[name], "divergence for %s", name)

		wantAdded, wantDeleted, err := s.Engine.GetDiffStats(b)
		require.NoError(t, err)
		require.Equal(t, engine.DiffStat{Added: wantAdded, Deleted: wantDeleted}, diffs[name], "diff for %s", name)

		wantCommits, err := s.Engine.GetAllCommits(b, engine.CommitFormatReadable)
		require.NoError(t, err)
		require.Equal(t, wantCommits, commits[name], "commits for %s", name)
	}
}
