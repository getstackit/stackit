package actions

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/config"
	"github.com/getstackit/stackit/internal/engine"
	stackiterrors "github.com/getstackit/stackit/internal/errors"
	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

type promptRestackHandler struct {
	recordingRestackHandler
	prompted         bool
	resolveConflicts bool
	promptErr        error
	onPrompt         func()
	conflicts        []string
}

func (h *promptRestackHandler) IsInteractive() bool { return true }

func (h *promptRestackHandler) PromptResolveConflicts(conflictBranches []string) (bool, error) {
	h.prompted = true
	h.conflicts = append([]string(nil), conflictBranches...)
	if h.onPrompt != nil {
		h.onPrompt()
	}
	return h.resolveConflicts, h.promptErr
}

// failingBranchWriteRunner fails every write to a branch ref, standing in for
// an unexpected (non-conflict) error partway through applying a restack.
type failingBranchWriteRunner struct {
	git.Runner
}

func (r *failingBranchWriteRunner) UpdateRefs(ctx context.Context, updates []git.RefUpdate, reflogMessage string) error {
	for _, update := range updates {
		if strings.HasPrefix(update.RefName, "refs/heads/") {
			return errors.New("disk full")
		}
	}
	return r.Runner.UpdateRefs(ctx, updates, reflogMessage)
}

// conflictScenario builds main -> feature where feature conflicts with a later
// trunk commit, returning the trunk revision before that commit.
func conflictScenario(t *testing.T) (*scenario.Scenario, string) {
	t.Helper()
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
	s.Checkout("main")
	require.NoError(t, s.Scene.Repo.CreateChangeAndCommit("base", "conflict"))
	s.CreateBranch("feature")
	require.NoError(t, s.Scene.Repo.CreateChangeAndCommit("feature change", "conflict"))
	s.TrackBranch("feature", "main")
	s.Checkout("main")
	mainBefore, err := s.Engine.GetRevision(t.Context(), engine.NewBranch("main", nil))
	require.NoError(t, err)
	require.NoError(t, s.Scene.Repo.CreateChangeAndCommit("main change", "conflict"))
	s.Checkout("feature")
	return s, mainBefore
}

// recordingRestackHandler keeps every event and the final summary.
type recordingRestackHandler struct {
	NullRestackHandler
	mu      sync.Mutex
	events  []RestackBranchEvent
	summary *RestackSummary
	// completions is every summary reported, to pin "exactly once".
	completions []RestackSummary
}

func (h *recordingRestackHandler) OnRestackBranch(event RestackBranchEvent) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.events = append(h.events, event)
}

func (h *recordingRestackHandler) OnRestackComplete(summary RestackSummary) {
	h.summary = &summary
	h.completions = append(h.completions, summary)
}

func (h *recordingRestackHandler) branchesWith(result RestackResult) []string {
	var names []string
	for _, e := range h.events {
		if e.Result == result {
			names = append(names, e.Branch)
		}
	}
	return names
}

func TestRestackAction(t *testing.T) {
	t.Parallel()
	t.Run("planning from trunk excludes trunk branch", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

		plan, err := PlanRestack(s.Context, restackDepsFrom(s.Context), RestackOptions{
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

		plan, err := PlanRestack(s.Context, restackDepsFrom(s.Context), RestackOptions{
			BranchName: "main",
			Scope:      engine.StackRangeFull(),
		})
		require.NoError(t, err)
		require.True(t, plan.HasBranches())
		require.Equal(t, 2, plan.BranchCount())

		var names []string
		for _, group := range plan.groups {
			for _, branch := range group.sortedBranches {
				names = append(names, branch.Name())
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

		recorder := &recordingRestackHandler{}
		plan, err := PlanRestack(s.Context, restackDepsFrom(s.Context), RestackOptions{
			AllStacks: true,
			Parallel:  true,
			Jobs:      2,
		})
		require.NoError(t, err)
		plan.newWorktreeEngine = func(context.Context, engine.WorktreeEngineOptions) (engine.Engine, error) {
			return nil, errors.New("boom")
		}
		err = RestackAction(s.Context, restackDepsFrom(s.Context), plan, recorder)
		require.Error(t, err)
		require.ErrorContains(t, err, "restack failed")
		require.ErrorContains(t, err, "alpha-root")
		require.ErrorContains(t, err, "beta-root")
		require.ErrorContains(t, err, "create worktree engine")

		// Every branch in a failed group must appear in the summary so users
		// don't see "skipped=0" while entire stacks silently failed to start.
		conflictBranches := recorder.branchesWith(RestackConflict)
		require.ElementsMatch(t,
			[]string{"alpha-root", "alpha-child", "beta-root"},
			conflictBranches,
		)
		require.NotNil(t, recorder.summary)
		require.ElementsMatch(t, conflictBranches, recorder.summary.Conflicts)
	})

	t.Run("planning errors on a nonexistent branch", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

		_, err := PlanRestack(s.Context, restackDepsFrom(s.Context), RestackOptions{
			BranchName: "does-not-exist",
			Scope:      engine.StackRangeFull(),
		})
		require.ErrorContains(t, err, "branch does-not-exist does not exist")
	})

	t.Run("non-interactive restack reports conflicts without entering the workflow", func(t *testing.T) {
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

		plan, err := PlanRestack(s.Context, restackDepsFrom(s.Context), RestackOptions{
			BranchName:     "feature",
			Scope:          engine.StackRange{IncludeCurrent: true},
			NonInteractive: true,
		})
		require.NoError(t, err)
		require.True(t, plan.HasWork())

		recorder := &recordingRestackHandler{}
		err = RestackAction(s.Context, restackDepsFrom(s.Context), plan, recorder)

		// A routine conflict is data, not an error: status "conflict" with
		// the branch listed, and the repo left clean — never mid-rebase with
		// human conflict guidance printed into the JSON stream.
		require.NoError(t, err)
		require.Equal(t, []string{"feature"}, recorder.branchesWith(RestackConflict))
		require.NotNil(t, recorder.summary)
		require.Equal(t, []string{"feature"}, recorder.summary.Conflicts)
		require.False(t, s.Git.IsRebaseInProgress(t.Context()))
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
		bBefore, err := s.Engine.GetRevision(t.Context(), engine.NewBranch("b", nil))
		require.NoError(t, err)

		s.Checkout("main")
		require.NoError(t, s.Scene.Repo.CreateChangeAndCommit("main change", "conflict"))
		s.Checkout("b")

		plan, err := PlanRestack(s.Context, restackDepsFrom(s.Context), RestackOptions{
			BranchName: "b",
			Scope:      engine.StackRangeFull(),
		})
		require.NoError(t, err)
		require.True(t, plan.HasWork())

		handler := &promptRestackHandler{resolveConflicts: true}
		err = RestackAction(s.Context, restackDepsFrom(s.Context), plan, handler)
		require.True(t, handler.prompted)
		require.Equal(t, []string{"b"}, handler.conflicts)

		// Answering "yes" must actually enter the conflict workflow: the
		// held-back ancestors get applied first, then b's rebase stops in
		// conflict state. Before the fix this errored with "expected conflict
		// on b but rebase completed successfully" and left HEAD detached.
		require.ErrorIs(t, err, stackiterrors.ErrConflictWorkflow)
		// The workflow's continue/abort guidance must stay the last output.
		require.Empty(t, handler.completions)
		require.True(t, s.Git.IsRebaseInProgress(t.Context()))

		continuation, err := config.GetContinuationState(s.Scene.Dir)
		require.NoError(t, err)
		require.Equal(t, bBefore, continuation.ExpectedBranchRevision)

		mainRev, err := s.Engine.GetRevision(t.Context(), engine.NewBranch("main", nil))
		require.NoError(t, err)
		isAncestor, err := s.Git.IsAncestor(t.Context(), mainRev, "a")
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
		featureBefore, err := s.Engine.GetRevision(t.Context(), engine.NewBranch("feature", nil))
		require.NoError(t, err)

		s.Checkout("main")
		require.NoError(t, s.Scene.Repo.CreateChangeAndCommit("main change", "conflict"))
		s.Checkout("feature")

		plan, err := PlanRestack(s.Context, restackDepsFrom(s.Context), RestackOptions{
			BranchName: "feature",
			Scope:      engine.StackRange{IncludeCurrent: true},
		})
		require.NoError(t, err)
		require.True(t, plan.HasWork())

		handler := &promptRestackHandler{}
		err = RestackAction(s.Context, restackDepsFrom(s.Context), plan, handler)
		require.NoError(t, err)
		require.True(t, handler.prompted)
		require.Equal(t, []string{"feature"}, handler.conflicts)
		require.False(t, s.Git.IsRebaseInProgress(t.Context()))

		featureRev, err := s.Engine.GetRevision(t.Context(), engine.NewBranch("feature", nil))
		require.NoError(t, err)
		require.Equal(t, featureBefore, featureRev)
	})

	t.Run("unexpected restack error completes as failed", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{"feature": "main"})
		s.Checkout("main").CommitChange("trunk-update", "advance trunk")
		s.Checkout("feature")

		eng, err := engine.NewEngine(t.Context(), engine.Options{
			RepoRoot: s.Scene.Dir,
			Trunk:    "main",
			Git:      &failingBranchWriteRunner{Runner: s.Git},
		})
		require.NoError(t, err)
		s.Context.Engine = eng

		plan, err := PlanRestack(s.Context, restackDepsFrom(s.Context), RestackOptions{
			BranchName: "feature",
			Scope:      engine.StackRange{IncludeCurrent: true},
		})
		require.NoError(t, err)
		handler := &recordingRestackHandler{}
		err = RestackAction(s.Context, restackDepsFrom(s.Context), plan, handler)
		require.ErrorContains(t, err, "restack failed")
		require.NotErrorIs(t, err, stackiterrors.ErrConflictWorkflow)
		require.Len(t, handler.completions, 1)
		require.True(t, handler.completions[0].Failed)
	})

	t.Run("non-interactive conflict workflow handoff skips the completion", func(t *testing.T) {
		t.Parallel()
		s, _ := conflictScenario(t)
		plan, err := PlanRestack(s.Context, restackDepsFrom(s.Context), RestackOptions{
			BranchName: "feature",
			Scope:      engine.StackRange{IncludeCurrent: true},
		})
		require.NoError(t, err)
		handler := &recordingRestackHandler{}
		err = RestackAction(s.Context, restackDepsFrom(s.Context), plan, handler)
		require.ErrorIs(t, err, stackiterrors.ErrConflictWorkflow)
		require.Empty(t, handler.completions)
	})

	t.Run("canceling the conflict prompt reports incomplete, not failed", func(t *testing.T) {
		t.Parallel()
		s, _ := conflictScenario(t)
		plan, err := PlanRestack(s.Context, restackDepsFrom(s.Context), RestackOptions{
			BranchName: "feature",
			Scope:      engine.StackRange{IncludeCurrent: true},
		})
		require.NoError(t, err)
		handler := &promptRestackHandler{promptErr: stackiterrors.ErrCanceled}
		err = RestackAction(s.Context, restackDepsFrom(s.Context), plan, handler)
		require.NoError(t, err)
		require.True(t, handler.prompted)
		require.Len(t, handler.completions, 1)
		require.False(t, handler.completions[0].Failed)
		require.Equal(t, []string{"feature"}, handler.completions[0].Conflicts)
		require.False(t, s.Git.IsRebaseInProgress(t.Context()))
	})

	t.Run("conflict workflow that finishes without stopping still completes", func(t *testing.T) {
		t.Parallel()
		s, mainBefore := conflictScenario(t)
		plan, err := PlanRestack(s.Context, restackDepsFrom(s.Context), RestackOptions{
			BranchName: "feature",
			Scope:      engine.StackRange{IncludeCurrent: true},
		})
		require.NoError(t, err)
		// Rewinding trunk while the prompt is open makes the workflow find
		// nothing to stop on — the same outcome as rerere resolving it.
		handler := &promptRestackHandler{resolveConflicts: true, onPrompt: func() {
			s.RunGit("update-ref", "refs/heads/main", mainBefore)
		}}
		err = RestackAction(s.Context, restackDepsFrom(s.Context), plan, handler)
		require.NoError(t, err)
		require.Len(t, handler.completions, 1)
		summary := handler.completions[0]
		require.False(t, summary.Failed)
		require.Empty(t, summary.Conflicts)
		require.Equal(t, 1, summary.Restacked)
		require.False(t, s.Git.IsRebaseInProgress(t.Context()))
	})
}

// TestConflictedStack pins the branch set the prompt hands to the conflict
// workflow. Sync restacks every reparented branch as one group, so the group can
// span stacks; re-running an unrelated stack in EnterWorkflow mode could stop on
// its conflict instead of the one the user chose to resolve.
func TestConflictedStack(t *testing.T) {
	t.Parallel()
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
		WithStack(map[string]string{
			"a":       "main",
			"a-child": "a",
			"b":       "main",
		})

	eng := s.Context.Engine
	group := restackPlannedGroup{sortedBranches: eng.SortBranchesTopologically(t.Context(),
		engine.BranchesFromNames(eng, []string{"a", "a-child", "b"}),
	)}

	stack := conflictedStack(eng, []restackPlannedGroup{group}, "a-child")
	require.Equal(t, []string{"a", "a-child"}, stack.Names())
	require.Empty(t, conflictedStack(eng, []restackPlannedGroup{group}, "missing"))
}
