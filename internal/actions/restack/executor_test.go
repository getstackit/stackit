package restack_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/actions/restack"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

func TestExecutorHoldsBackConflictedStacks(t *testing.T) {
	t.Parallel()
	// main -> a -> b, and an unrelated stack main -> x.
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).WithLinearStack("a", "b")
	s.Checkout("main").CreateBranch("x").Commit("x change").TrackBranch("x", "main").Checkout("main")

	var got []restack.Progress
	var restacked []string
	exec := restack.Executor{
		Engine: s.Engine,
		Log:    s.Context.Logger,
		Report: func(p restack.Progress) {
			got = append(got, p)
			if p.Result == engine.RestackDone || p.Result == engine.RestackUnneeded {
				restacked = append(restacked, p.Branch)
			}
		},
	}

	a, b, x := s.Engine.GetBranch("a"), s.Engine.GetBranch("b"), s.Engine.GetBranch("x")
	plan := &restack.Plan{
		Branches:  engine.BranchesOf(a, b, x),
		Engine:    &engine.RestackPlan{ApplyMap: map[string]bool{"a": true, "b": true, "x": true}},
		Apply:     engine.BranchesOf(b, x),
		Conflicts: []string{"a"},
	}
	require.NoError(t, exec.Execute(t.Context(), plan, restack.ConflictContinue, nil))

	// b shares a's stack, so a's conflict holds it back; x is independent and
	// still applies.
	require.NotContains(t, restacked, "b")
	require.Contains(t, got, restack.Progress{Branch: "a", Result: engine.RestackConflict, Conflict: true})
	require.Contains(t, got, restack.Progress{Branch: "b", Result: engine.RestackBlocked})
}
