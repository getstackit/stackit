package shippable

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/actions/merge"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

type countingRemoteRunner struct {
	git.Runner
	fetchRemoteShas atomic.Int64
}

func (c *countingRemoteRunner) FetchRemoteShas(ctx context.Context, remote string) (map[string]string, error) {
	c.fetchRemoteShas.Add(1)
	return c.Runner.FetchRemoteShas(ctx, remote)
}

func newCountingAnalyzer(t *testing.T, dir string) (*Analyzer, engine.Engine, *countingRemoteRunner) {
	t.Helper()

	counting := &countingRemoteRunner{Runner: git.NewRunnerWithPath(dir, nil)}
	eng, err := engine.NewEngine(engine.Options{
		RepoRoot: dir,
		Trunk:    "main",
		Git:      counting,
	})
	require.NoError(t, err)

	return NewAnalyzer(eng, nil), eng, counting
}

func TestAnalyzerSkipsRemoteStatusForIncompleteStacks(t *testing.T) {
	t.Parallel()

	t.Run("missing PRs", func(t *testing.T) {
		t.Parallel()

		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{"P": "main", "C": "P"})
		analyzer, _, counting := newCountingAnalyzer(t, s.Scene.Dir)

		stack, err := analyzer.AnalyzeStack(context.Background(), merge.MultiStackInfo{
			RootBranch:  "P",
			AllBranches: []string{"P", "C"},
		})
		require.NoError(t, err)

		require.Equal(t, StatusIncomplete, stack.Status)
		require.Equal(t, int64(0), counting.fetchRemoteShas.Load(),
			"stacks with no PRs should not read remote branch status")
	})

	t.Run("draft PRs", func(t *testing.T) {
		t.Parallel()

		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{"P": "main", "C": "P"})
		analyzer, eng, counting := newCountingAnalyzer(t, s.Scene.Dir)
		require.NoError(t, eng.UpsertPrInfo(context.Background(), eng.GetBranch("P"), testhelpers.NewTestPrInfoDraft(101)))
		require.NoError(t, eng.UpsertPrInfo(context.Background(), eng.GetBranch("C"), testhelpers.NewTestPrInfoDraft(102)))

		stack, err := analyzer.AnalyzeStack(context.Background(), merge.MultiStackInfo{
			RootBranch:  "P",
			AllBranches: []string{"P", "C"},
		})
		require.NoError(t, err)

		require.Equal(t, StatusIncomplete, stack.Status)
		require.Equal(t, int64(0), counting.fetchRemoteShas.Load(),
			"draft-only stacks should not read remote branch status")
	})
}

func TestAnalyzerReadsRemoteStatusOnceForUpdateStack(t *testing.T) {
	t.Parallel()

	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
		WithStack(map[string]string{"P": "main", "C": "P"})
	analyzer, eng, counting := newCountingAnalyzer(t, s.Scene.Dir)
	require.NoError(t, eng.UpsertPrInfo(context.Background(), eng.GetBranch("P"), testhelpers.NewTestPrInfo(101)))
	require.NoError(t, eng.UpsertPrInfo(context.Background(), eng.GetBranch("C"), testhelpers.NewTestPrInfo(102)))

	stack, err := analyzer.AnalyzeStack(context.Background(), merge.MultiStackInfo{
		RootBranch:  "P",
		AllBranches: []string{"P", "C"},
	})
	require.NoError(t, err)

	require.Equal(t, StatusBlocked, stack.Status)
	require.Equal(t, int64(1), counting.fetchRemoteShas.Load(),
		"update stacks should read remote branch status once for the whole stack")
}

func TestAnalyzeAllReadsRemoteStatusOnceAcrossStacks(t *testing.T) {
	t.Parallel()

	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
		WithStack(map[string]string{"P1": "main", "P2": "main"})
	analyzer, eng, counting := newCountingAnalyzer(t, s.Scene.Dir)
	require.NoError(t, eng.UpsertPrInfo(context.Background(), eng.GetBranch("P1"), testhelpers.NewTestPrInfo(101)))
	require.NoError(t, eng.UpsertPrInfo(context.Background(), eng.GetBranch("P2"), testhelpers.NewTestPrInfo(102)))

	result, err := analyzer.AnalyzeAll(context.Background())
	require.NoError(t, err)

	require.Len(t, result.Stacks, 2)
	require.Equal(t, int64(1), counting.fetchRemoteShas.Load(),
		"AnalyzeAll should read remote branch status once for all stacks, not once per stack")
}

func TestAnalyzeAllLocalUsesOnlyCachedMetadata(t *testing.T) {
	t.Parallel()

	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
		WithStack(map[string]string{"P": "main"})
	analyzer, eng, counting := newCountingAnalyzer(t, s.Scene.Dir)
	require.NoError(t, eng.UpsertPrInfo(context.Background(), eng.GetBranch("P"), testhelpers.NewTestPrInfo(101)))

	result, err := analyzer.AnalyzeAllLocal()
	require.NoError(t, err)
	require.Len(t, result.Stacks, 1)
	require.Equal(t, StatusShippable, result.Stacks[0].Status)
	require.Equal(t, int64(0), counting.fetchRemoteShas.Load(),
		"local analysis must not contact a remote")
}
