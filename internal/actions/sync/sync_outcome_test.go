package sync

import (
	"testing"

	"github.com/stretchr/testify/require"

	stackErrors "github.com/getstackit/stackit/internal/errors"
	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

// outcomeHandler records completions and answers the conflict prompt.
type outcomeHandler struct {
	NullHandler
	completions []Summary
	resolve     bool
	promptErr   error
	onPrompt    func()
	prompted    bool
}

func (h *outcomeHandler) IsInteractive() bool { return true }

func (h *outcomeHandler) Complete(summary Summary) { h.completions = append(h.completions, summary) }

func (h *outcomeHandler) PromptResolveConflicts([]string) (bool, error) {
	h.prompted = true
	if h.onPrompt != nil {
		h.onPrompt()
	}
	return h.resolve, h.promptErr
}

// conflictedSyncScenario builds main -> P -> [0-Success, 1-Failure] where a new
// commit on P conflicts with 1-Failure, returning P's revision before it.
func conflictedSyncScenario(t *testing.T) (*scenario.Scenario, string) {
	t.Helper()
	s := scenario.NewRemoteScenario(t)
	s.CreateBranch("P").Commit("P change").TrackBranch("P", "main")
	s.Checkout("P").CreateBranch("0-Success").Commit("Success change").TrackBranch("0-Success", "P")
	s.Checkout("P").CreateBranch("1-Failure")
	require.NoError(t, s.Scene.Repo.CreateChangeAndCommit("initial content", "conflict"))
	s.TrackBranch("1-Failure", "P")
	s.Checkout("P")
	pBefore, err := s.Scene.Repo.RunGitCommandAndGetOutput("rev-parse", "HEAD")
	require.NoError(t, err)
	require.NoError(t, s.Scene.Repo.CreateChangeAndCommit("conflicting content", "conflict"))
	require.NoError(t, s.Engine.Rebuild("main"))
	return s, pBefore
}

func TestSyncActionOutcome(t *testing.T) {
	t.Parallel()

	t.Run("early failure completes as failed", func(t *testing.T) {
		t.Parallel()
		// No remote: the trunk fetch fails before any later phase runs.
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		handler := &outcomeHandler{}
		err := Action(s.Context, Options{}, handler)
		require.ErrorContains(t, err, "failed to fetch trunk from")
		require.Len(t, handler.completions, 1)
		require.True(t, handler.completions[0].Failed)
	})

	t.Run("canceling the conflict prompt reports incomplete, not failed", func(t *testing.T) {
		t.Parallel()
		s, _ := conflictedSyncScenario(t)
		handler := &outcomeHandler{promptErr: stackErrors.ErrCanceled}
		err := Action(s.Context, Options{Restack: true}, handler)
		require.NoError(t, err)
		require.True(t, handler.prompted)
		require.Len(t, handler.completions, 1)
		require.False(t, handler.completions[0].Failed)
		require.Equal(t, []string{"1-Failure"}, handler.completions[0].ConflictBranches)
	})

	t.Run("conflict workflow handoff skips the completion", func(t *testing.T) {
		t.Parallel()
		s, _ := conflictedSyncScenario(t)
		handler := &outcomeHandler{resolve: true}
		err := Action(s.Context, Options{Restack: true}, handler)
		require.ErrorIs(t, err, stackErrors.ErrConflictWorkflow)
		require.Empty(t, handler.completions)
	})

	t.Run("conflict workflow that finishes without stopping still completes", func(t *testing.T) {
		t.Parallel()
		s, pBefore := conflictedSyncScenario(t)
		// Dropping P's conflicting commit while the prompt is open makes the
		// workflow find nothing to stop on — the same outcome as rerere
		// resolving every conflict.
		handler := &outcomeHandler{resolve: true, onPrompt: func() {
			s.RunGit("reset", "--hard", pBefore)
		}}
		err := Action(s.Context, Options{Restack: true}, handler)
		require.NoError(t, err)
		require.Len(t, handler.completions, 1)
		summary := handler.completions[0]
		require.False(t, summary.Failed)
		require.Empty(t, summary.ConflictBranches)
		require.Zero(t, summary.BranchesSkipped)
		// The workflow re-runs only 1-Failure's line (P, 1-Failure); the
		// sibling 0-Success it held back stays reported as blocked.
		require.Equal(t, 1, summary.BranchesBlocked)
		require.Positive(t, summary.BranchesRestacked)
	})
}
