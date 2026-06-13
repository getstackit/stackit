package engine_test

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

// countingFetchRunner wraps a git.Runner and counts FetchRemoteShas calls, so a
// test can assert remote status is read once for a whole stack rather than once
// per branch.
type countingFetchRunner struct {
	git.Runner
	fetchRemoteShas atomic.Int64
}

func (c *countingFetchRunner) FetchRemoteShas(ctx context.Context, remote string) (map[string]string, error) {
	c.fetchRemoteShas.Add(1)
	return c.Runner.FetchRemoteShas(ctx, remote)
}

func newCountingEngine(t *testing.T, dir string) (engine.Engine, *countingFetchRunner) {
	t.Helper()
	counting := &countingFetchRunner{Runner: git.NewRunnerWithPath(dir, nil)}
	eng, err := engine.NewEngine(engine.Options{
		RepoRoot: dir,
		Trunk:    "main",
		Git:      counting,
	})
	require.NoError(t, err)
	return eng, counting
}

func TestBatchGetPRSubmissionStatusReadsRemoteOnceForUpdates(t *testing.T) {
	t.Parallel()

	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
		WithStack(map[string]string{"P": "main", "C1": "P", "C2": "C1"})
	_, err := s.Scene.Repo.CreateBareRemote("origin")
	require.NoError(t, err)
	for _, b := range []string{"main", "P", "C1", "C2"} {
		require.NoError(t, s.Scene.Repo.PushBranch("origin", b))
	}

	eng, counting := newCountingEngine(t, s.Scene.Dir)

	// Give each branch an existing PR so it is an update, not a create.
	for i, name := range []string{"P", "C1", "C2"} {
		require.NoError(t, eng.UpsertPrInfo(context.Background(), eng.GetBranch(name),
			testhelpers.NewTestPrInfoWithTitle(100+i, "title")))
	}

	branches := engine.BranchesOf(eng.GetBranch("P"), eng.GetBranch("C1"), eng.GetBranch("C2"))

	// Measure only the batched call, isolating it from any setup reads.
	counting.fetchRemoteShas.Store(0)
	statuses, err := eng.BatchGetPRSubmissionStatus(branches)
	require.NoError(t, err)
	require.Len(t, statuses, 3)

	require.Equal(t, int64(1), counting.fetchRemoteShas.Load(),
		"submission status for an update stack must read remote once, not per branch")
}

func TestBatchUpsertPrInfo(t *testing.T) {
	t.Parallel()

	t.Run("writes all branches in one transaction", func(t *testing.T) {
		t.Parallel()

		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{"P": "main", "C1": "P", "C2": "C1"})

		eng := s.Engine

		updates := map[string]*engine.PrInfo{
			"P":  testhelpers.NewTestPrInfoWithTitle(101, "PR for P").WithMergeBranch("consolidation"),
			"C1": testhelpers.NewTestPrInfoWithTitle(102, "PR for C1").WithMergeBranch("consolidation"),
			"C2": testhelpers.NewTestPrInfoWithTitle(103, "PR for C2").WithMergeBranch("consolidation"),
		}

		require.NoError(t, eng.BatchUpsertPrInfo(context.Background(), updates))

		for name, want := range updates {
			got, err := eng.GetBranch(name).GetPrInfo()
			require.NoError(t, err)
			require.NotNil(t, got, "branch %s should have PR info", name)
			require.Equal(t, want.Title(), got.Title(), "branch %s title", name)
			require.Equal(t, want.MergeBranch(), got.MergeBranch(), "branch %s merge branch", name)
		}
	})

	t.Run("preserves existing fields when merging", func(t *testing.T) {
		t.Parallel()

		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{"A": "main"})

		eng := s.Engine
		branch := eng.GetBranch("A")

		// Set initial PR info with a title and state
		require.NoError(t, eng.UpsertPrInfo(context.Background(), branch,
			testhelpers.NewTestPrInfoFull(200, "original title", "body", "OPEN", "main", "https://example.com", false)))

		// Batch upsert only updates MergeBranch; other fields should be preserved
		prInfo, err := branch.GetPrInfo()
		require.NoError(t, err)
		require.NoError(t, eng.BatchUpsertPrInfo(context.Background(), map[string]*engine.PrInfo{
			"A": prInfo.WithMergeBranch("my-consolidation"),
		}))

		got, err := branch.GetPrInfo()
		require.NoError(t, err)
		require.Equal(t, "original title", got.Title())
		require.Equal(t, "my-consolidation", got.MergeBranch())
	})

	t.Run("no-op on empty map", func(t *testing.T) {
		t.Parallel()

		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{"A": "main"})

		require.NoError(t, s.Engine.BatchUpsertPrInfo(context.Background(), nil))
		require.NoError(t, s.Engine.BatchUpsertPrInfo(context.Background(), map[string]*engine.PrInfo{}))
	})
}

func TestBatchGetPRSubmissionStatusSkipsRemoteForCreates(t *testing.T) {
	t.Parallel()

	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
		WithStack(map[string]string{"P": "main", "C1": "P", "C2": "C1"})

	eng, counting := newCountingEngine(t, s.Scene.Dir)

	branches := engine.BranchesOf(eng.GetBranch("P"), eng.GetBranch("C1"), eng.GetBranch("C2"))

	counting.fetchRemoteShas.Store(0)
	statuses, err := eng.BatchGetPRSubmissionStatus(branches)
	require.NoError(t, err)
	require.Len(t, statuses, 3)

	require.Equal(t, int64(0), counting.fetchRemoteShas.Load(),
		"an all-creates stack needs no remote read for submission status")
	for name, st := range statuses {
		require.Equal(t, "create", st.Action, "branch %s should be a create", name)
	}
}
