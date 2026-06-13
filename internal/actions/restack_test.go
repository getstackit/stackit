package actions

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/handlers"
	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

type promptRestackHandler struct {
	handlers.NullRestackHandler
	prompted         bool
	resolveConflicts bool
	conflicts        []string
}

func (h *promptRestackHandler) IsInteractive() bool { return true }

func (h *promptRestackHandler) PromptResolveConflicts(conflictBranches []string) (bool, error) {
	h.prompted = true
	h.conflicts = append([]string(nil), conflictBranches...)
	return h.resolveConflicts, nil
}

func TestRestackAction(t *testing.T) {
	t.Parallel()
	t.Run("planning from trunk excludes trunk branch", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

		plan, err := PlanRestack(s.Context, RestackOptions{
			BranchName: "main",
			Scope:      engine.StackRangeFull(),
		})
		require.NoError(t, err)
		require.False(t, plan.HasBranches())
		require.Equal(t, 0, plan.BranchCount())
	})

	t.Run("planning from trunk keeps descendants but not trunk", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"feature":       "main",
				"feature-child": "feature",
			})

		plan, err := PlanRestack(s.Context, RestackOptions{
			BranchName: "main",
			Scope:      engine.StackRangeFull(),
		})
		require.NoError(t, err)
		require.True(t, plan.HasBranches())
		require.Equal(t, 2, plan.BranchCount())

		var names []string
		for _, group := range plan.groups {
			for _, branch := range group.sortedBranches {
				names = append(names, branch.GetName())
			}
		}
		require.Equal(t, []string{"feature", "feature-child"}, names)
		require.NotContains(t, names, "main")
	})

	t.Run("parallel multi-stack restack returns worker errors", func(t *testing.T) {
		t.Parallel()
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

	t.Run("interactive restack prompts before entering conflict workflow", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

		s.Checkout("main")
		require.NoError(t, s.Scene.Repo.CreateChangeAndCommit("base", "conflict"))

		s.CreateBranch("feature")
		require.NoError(t, s.Scene.Repo.CreateChangeAndCommit("feature change", "conflict"))
		s.TrackBranch("feature", "main")
		featureBefore, err := s.Engine.GetRevision(engine.NewBranch("feature", nil))
		require.NoError(t, err)

		s.Checkout("main")
		require.NoError(t, s.Scene.Repo.CreateChangeAndCommit("main change", "conflict"))
		s.Checkout("feature")

		plan, err := PlanRestack(s.Context, RestackOptions{
			BranchName: "feature",
			Scope:      engine.StackRange{IncludeCurrent: true},
		})
		require.NoError(t, err)
		require.True(t, plan.HasWork())

		handler := &promptRestackHandler{}
		err = RestackAction(s.Context, plan, handler)
		require.NoError(t, err)
		require.True(t, handler.prompted)
		require.Equal(t, []string{"feature"}, handler.conflicts)
		require.False(t, s.Engine.Git().IsRebaseInProgress(context.Background()))

		featureRev, err := s.Engine.GetRevision(engine.NewBranch("feature", nil))
		require.NoError(t, err)
		require.Equal(t, featureBefore, featureRev)
	})
}
