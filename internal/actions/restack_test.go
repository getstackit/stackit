package actions

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/engine"
	stackiterrors "github.com/getstackit/stackit/internal/errors"
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

	t.Run("JSON restack reports conflicts without entering the workflow", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

		s.Checkout("main")
		require.NoError(t, s.Scene.Repo.CreateChangeAndCommit("base", "conflict"))
		s.CreateBranch("feature")
		require.NoError(t, s.Scene.Repo.CreateChangeAndCommit("feature change", "conflict"))
		s.TrackBranch("feature", "main")
		s.Checkout("main")
		require.NoError(t, s.Scene.Repo.CreateChangeAndCommit("main change", "conflict"))
		s.Checkout("feature")

		plan, err := PlanRestack(s.Context, RestackOptions{
			BranchName: "feature",
			Scope:      engine.StackRange{IncludeCurrent: true},
		})
		require.NoError(t, err)
		require.True(t, plan.HasWork())

		jsonHandler := handlers.NewJSONRestackHandler()
		err = RestackAction(s.Context, plan, jsonHandler)

		// A routine conflict is data, not an error: status "conflict" with
		// the branch listed, and the repo left clean — never mid-rebase with
		// human conflict guidance printed into the JSON stream.
		require.NoError(t, err)
		require.Equal(t, handlers.RestackJSONStatusConflict, jsonHandler.Result.Status)
		require.Equal(t, 1, jsonHandler.Result.ConflictCount)
		require.Equal(t, "feature", jsonHandler.Result.Conflicts[0].Branch)
		require.False(t, s.Engine.Git().IsRebaseInProgress(context.Background()))
	})

	t.Run("resolving mid-stack conflict applies ancestors before entering workflow", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

		s.Checkout("main")
		require.NoError(t, s.Scene.Repo.CreateChangeAndCommit("base", "conflict"))

		// a rebases cleanly; b (child of a) conflicts with the trunk change.
		s.CreateBranch("a")
		require.NoError(t, s.Scene.Repo.CreateChangeAndCommit("a change", "afile"))
		s.TrackBranch("a", "main")

		s.CreateBranch("b")
		require.NoError(t, s.Scene.Repo.CreateChangeAndCommit("b change", "conflict"))
		s.TrackBranch("b", "a")

		s.Checkout("main")
		require.NoError(t, s.Scene.Repo.CreateChangeAndCommit("main change", "conflict"))
		s.Checkout("b")

		plan, err := PlanRestack(s.Context, RestackOptions{
			BranchName: "b",
			Scope:      engine.StackRangeFull(),
		})
		require.NoError(t, err)
		require.True(t, plan.HasWork())

		handler := &promptRestackHandler{resolveConflicts: true}
		err = RestackAction(s.Context, plan, handler)
		require.True(t, handler.prompted)
		require.Equal(t, []string{"b"}, handler.conflicts)

		// Answering "yes" must actually enter the conflict workflow: the
		// held-back ancestors get applied first, then b's rebase stops in
		// conflict state. Before the fix this errored with "expected conflict
		// on b but rebase completed successfully" and left HEAD detached.
		require.ErrorIs(t, err, stackiterrors.ErrConflictWorkflow)
		require.True(t, s.Engine.Git().IsRebaseInProgress(context.Background()))

		mainRev, err := s.Engine.GetRevision(engine.NewBranch("main", nil))
		require.NoError(t, err)
		isAncestor, err := s.Engine.Git().IsAncestor(context.Background(), mainRev, "a")
		require.NoError(t, err)
		require.True(t, isAncestor, "ancestor a must be restacked onto the new trunk before entering the conflict workflow")
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
