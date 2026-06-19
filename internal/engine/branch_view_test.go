package engine_test

import (
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
