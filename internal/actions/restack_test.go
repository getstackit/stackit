package actions

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/handlers"
	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

func TestRestackAction(t *testing.T) {
	t.Run("parallel multi-stack restack returns worker errors", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"alpha-root":  "main",
				"alpha-child": "alpha-root",
				"beta-root":   "main",
			})

		originalNewWorktreeEngine := newWorktreeEngine
		newWorktreeEngine = func(engine.WorktreeEngineOptions) (engine.Engine, error) {
			return nil, errors.New("boom")
		}
		t.Cleanup(func() {
			newWorktreeEngine = originalNewWorktreeEngine
		})

		jsonHandler := handlers.NewJSONRestackHandler()
		plan, err := PlanRestack(s.Context, RestackOptions{
			AllStacks: true,
			Parallel:  true,
			Jobs:      2,
		})
		require.NoError(t, err)
		err = RestackAction(s.Context, plan, jsonHandler)
		require.Error(t, err)
		require.ErrorContains(t, err, "restack failed")
		require.ErrorContains(t, err, "alpha-root")
		require.ErrorContains(t, err, "beta-root")
		require.ErrorContains(t, err, "create worktree engine")

		// Every branch in a failed group must appear in the summary so users
		// don't see "skipped=0" while entire stacks silently failed to start.
		conflictBranches := make([]string, 0, len(jsonHandler.Result.Conflicts))
		for _, c := range jsonHandler.Result.Conflicts {
			conflictBranches = append(conflictBranches, c.Branch)
		}
		require.ElementsMatch(t,
			[]string{"alpha-root", "alpha-child", "beta-root"},
			conflictBranches,
		)
		require.Equal(t, len(conflictBranches), jsonHandler.Result.ConflictCount)
	})
}
