package shippable

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/actions/stackview"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

type countingRemoteRunner struct {
	git.Runner
	fetchRemoteShas atomic.Int64
}

func (c *countingRemoteRunner) FetchRemoteShas(ctx context.Context, remote string) (git.RemoteBranchSHAs, error) {
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

		stack, err := analyzer.AnalyzeStack(context.Background(), stackview.StackInfo{
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

		stack, err := analyzer.AnalyzeStack(context.Background(), stackview.StackInfo{
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

	stack, err := analyzer.AnalyzeStack(context.Background(), stackview.StackInfo{
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

func TestAnalyzeAllLocalNeverReportsShippable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		prInfo     *engine.PrInfo
		wantStatus Status
		wantReason BlockingReason
	}{
		{
			name:       "open PR is unverified without forge data",
			prInfo:     testhelpers.NewTestPrInfo(101),
			wantStatus: StatusUnverified,
		},
		{
			name:       "draft PR is incomplete",
			prInfo:     testhelpers.NewTestPrInfoDraft(101),
			wantStatus: StatusIncomplete,
			wantReason: ReasonDraft,
		},
		{
			name:       "closed PR is blocked",
			prInfo:     testhelpers.NewTestPrInfoClosed(101),
			wantStatus: StatusBlocked,
			wantReason: ReasonPRClosed,
		},
		{
			name:       "merged PR is blocked",
			prInfo:     testhelpers.NewTestPrInfoMerged(101, "main"),
			wantStatus: StatusBlocked,
			wantReason: ReasonPRMerged,
		},
		{
			name:       "missing PR is incomplete",
			wantStatus: StatusIncomplete,
			wantReason: ReasonNoPR,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
				WithStack(map[string]string{"P": "main"})
			analyzer, eng, counting := newCountingAnalyzer(t, s.Scene.Dir)
			if tt.prInfo != nil {
				require.NoError(t, eng.UpsertPrInfo(context.Background(), eng.GetBranch("P"), tt.prInfo))
			}

			result, err := analyzer.AnalyzeAllLocal()
			require.NoError(t, err)
			require.Len(t, result.Stacks, 1)
			stack := result.Stacks[0]
			require.Equal(t, tt.wantStatus, stack.Status)
			require.False(t, stack.ApprovalOK, "offline analysis cannot know review state")
			require.False(t, stack.GitHubCIOK, "offline analysis cannot know CI state")
			require.Zero(t, result.ShippableCount)
			if tt.wantReason == "" {
				require.Empty(t, stack.BlockingPRs)
				require.Equal(t, 1, result.UnverifiedCount)
			} else {
				require.Len(t, stack.BlockingPRs, 1)
				require.Equal(t, tt.wantReason, stack.BlockingPRs[0].Reason)
			}
			require.Equal(t, int64(0), counting.fetchRemoteShas.Load(),
				"local analysis must not contact a remote")
		})
	}
}

func TestAnalysisResultFilterRecountsStatuses(t *testing.T) {
	t.Parallel()

	result := &AnalysisResult{Stacks: []Stack{
		{Status: StatusUnverified},
		{Status: StatusBlocked},
		{Status: StatusUnverified},
	}}
	filtered := result.Filter(func(stack Stack) bool { return stack.Status == StatusUnverified })
	require.Len(t, filtered.Stacks, 2)
	require.Equal(t, 2, filtered.UnverifiedCount)
	require.Zero(t, filtered.BlockedCount)
}
