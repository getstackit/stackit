package engine_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

// TestBatchBranchStats asserts the batched stats match an independent raw-git
// oracle (commit count and diff numstat against each branch's parent), so
// annotation builders can rely on the batch reader as the single source.
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

		parent := b.GetParent().GetName()

		wantCount, err := s.Scene.Repo.GetCommitCount(parent, name)
		require.NoError(t, err)
		require.Equal(t, wantCount, stat.CommitCount, "CommitCount for %s", name)

		wantAdded, wantDeleted, err := s.Scene.Repo.GetDiffStats(parent, name)
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
// independent Git reads, so consumers can compose the value maps.
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

		wantAdded, wantDeleted, err := s.Scene.Repo.GetDiffStats(b.GetParent().GetName(), name)
		require.NoError(t, err)
		require.Equal(t, engine.DiffStat{Added: wantAdded, Deleted: wantDeleted}, diffs[name], "diff for %s", name)

		raw, err := s.Engine.Git().RunGitCommandWithContext(context.Background(), "log", "--format=%h %s", wantDiv+".."+name)
		require.NoError(t, err)
		require.Equal(t, strings.Split(raw, "\n"), commits.Commits[name], "commits for %s", name)
	}
}

// TestBatchCommitInfo asserts the batched commit info matches the per-branch
// raw Git date and author for every branch, including trunk.
func TestBatchCommitInfo(t *testing.T) {
	t.Parallel()
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
	s.WithLinearStack3()

	branches := s.Engine.AllBranches()
	info := s.Engine.BatchCommitInfo(branches)

	for _, b := range branches {
		name := b.GetName()
		got, ok := info[name]
		require.True(t, ok, "missing commit info for %s", name)

		wantDate, err := s.Engine.Git().RunGitCommandWithContext(context.Background(), "log", "-1", "--format=%aI", name)
		require.NoError(t, err)
		require.Equal(t, wantDate, got.Date.Format(time.RFC3339), "CommitDate for %s", name)

		wantAuthor, err := s.Engine.Git().RunGitCommandWithContext(context.Background(), "log", "-1", "--format=%an", name)
		require.NoError(t, err)
		require.Equal(t, wantAuthor, got.Author, "CommitAuthor for %s", name)
	}
}

// TestCommitsFallBackToParentTipWithoutStoredDivergence verifies that a branch
// with no recorded ParentBranchRevision lists only its own commits — measured
// against the parent's current tip — rather than its entire history back to the
// repo root (which an empty base would produce).
func TestCommitsFallBackToParentTipWithoutStoredDivergence(t *testing.T) {
	t.Parallel()
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
	s.WithLinearStack3() // main -> a -> b -> c

	// Clear b's stored divergence point but keep its parent (a), so the commit
	// base must fall back to the parent tip rather than an empty base.
	meta, err := s.Engine.Git().ReadMetadata("b")
	require.NoError(t, err)
	require.NoError(t, s.Engine.Git().WriteMetadata("b", meta.WithParentBranchRevision(nil)))
	require.NoError(t, s.Engine.Rebuild("main"))

	b := s.Engine.GetBranch("b")

	commits, err := s.Engine.BatchCommits(engine.BranchesOf(b), engine.CommitFormatReadable).ForBranch(b)
	require.NoError(t, err)

	// Without the fallback, a commit read walks b's whole history to the repo
	// root while a parent..b walk covers only b's own commits, so the two
	// disagree. The raw-git count against the parent tip is the oracle here.
	count, err := s.Scene.Repo.GetCommitCount(b.GetParent().GetName(), "b")
	require.NoError(t, err)
	require.Equal(t, count, len(commits),
		"commit messages and count must use the same base when no divergence is stored")
}
