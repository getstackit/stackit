package engine_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

type planningReadsGit struct {
	git.Runner
	ancestryBatches    []int
	individualAncestry int
	landingChecks      map[string]int
}

func (g *planningReadsGit) ReadAncestry(ctx context.Context, ranges ...git.RevRange) git.ReadResults[bool] {
	g.ancestryBatches = append(g.ancestryBatches, len(ranges))
	return g.Runner.ReadAncestry(ctx, ranges...)
}

func (g *planningReadsGit) IsAncestor(ctx context.Context, base, head string) (bool, error) {
	g.individualAncestry++
	return g.Runner.IsAncestor(ctx, base, head)
}

func (g *planningReadsGit) IsMerged(ctx context.Context, branch, target string) (bool, error) {
	g.landingChecks[branch+":"+target]++
	return g.Runner.IsMerged(ctx, branch, target)
}

func TestRestackPlanningSharesReadsOnlyWithinPlan(t *testing.T) {
	t.Parallel()
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
	s.WithLinearStack3()
	g := &planningReadsGit{Runner: git.NewRunnerWithPath(s.Scene.Dir, nil), landingChecks: make(map[string]int)}
	eng, err := engine.NewEngine(engine.Options{RepoRoot: s.Scene.Dir, Trunk: "main", Git: g})
	require.NoError(t, err)
	branches := engine.BranchesFromNames(eng, []string{"a", "b", "c"})
	plan, err := eng.PlanRestack(t.Context(), branches)
	require.NoError(t, err)
	require.Empty(t, plan.ApplyMap)
	require.Equal(t, []int{3}, g.ancestryBatches)
	require.Zero(t, g.individualAncestry)
	require.Equal(t, 1, g.landingChecks["a:main"], "the root is also checked as b's parent")

	// A new planning operation must observe refs changed since the first plan.
	s.Checkout("a").CommitChange("advanced.txt", "new root commit")
	plan, err = eng.PlanRestack(t.Context(), branches)
	require.NoError(t, err)
	require.NotEmpty(t, plan.ApplyMap)
	require.Equal(t, []int{3, 3}, g.ancestryBatches)
	require.Equal(t, 2, g.landingChecks["a:main"])
}
