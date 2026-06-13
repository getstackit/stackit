package untrack_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/actions/untrack"
	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

// testUntrackHandler is a test handler for untrack operations
type testUntrackHandler struct {
	confirmResult       bool
	confirmErr          error
	isInteractive       bool
	promptConfirmCalled bool
	lastBranchName      string
	lastDescendantCount int
}

func (h *testUntrackHandler) PromptConfirmUntrackDescendants(branchName string, descendantCount int) (bool, error) {
	h.promptConfirmCalled = true
	h.lastBranchName = branchName
	h.lastDescendantCount = descendantCount
	return h.confirmResult, h.confirmErr
}

func (h *testUntrackHandler) Cleanup() {}

func (h *testUntrackHandler) IsInteractive() bool { return h.isInteractive }

func TestUntrackAction(t *testing.T) {
	t.Parallel()
	t.Run("fails for untracked branch", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit().
			CreateBranchQuiet("untracked")

		err := untrack.Action(s.Context, untrack.Options{
			BranchName: "untracked",
		}, nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "is not tracked")
	})

	t.Run("succeeds for branch without descendants", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit().
			CreateBranch("feature").
			Commit("feature commit").
			TrackBranch("feature", "main")

		require.True(t, s.Engine.GetBranch("feature").IsTracked())

		err := untrack.Action(s.Context, untrack.Options{
			BranchName: "feature",
		}, nil)
		require.NoError(t, err)
		require.False(t, s.Engine.GetBranch("feature").IsTracked())
	})

	t.Run("force flag bypasses confirmation for descendants", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit().
			CreateBranch("feature").
			Commit("feature commit").
			TrackBranch("feature", "main").
			CreateBranch("child").
			Commit("child commit").
			TrackBranch("child", "feature").
			Checkout("feature")

		require.True(t, s.Engine.GetBranch("feature").IsTracked())
		require.True(t, s.Engine.GetBranch("child").IsTracked())

		handler := &testUntrackHandler{
			isInteractive: true,
			confirmResult: false, // Would decline if asked
		}

		err := untrack.Action(s.Context, untrack.Options{
			BranchName: "feature",
			Force:      true,
		}, handler)
		require.NoError(t, err)
		require.False(t, handler.promptConfirmCalled) // Force bypasses prompt
		require.False(t, s.Engine.GetBranch("feature").IsTracked())
		require.False(t, s.Engine.GetBranch("child").IsTracked())
	})

	t.Run("prompts for confirmation when descendants exist", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit().
			CreateBranch("feature").
			Commit("feature commit").
			TrackBranch("feature", "main").
			CreateBranch("child").
			Commit("child commit").
			TrackBranch("child", "feature").
			Checkout("feature")

		handler := &testUntrackHandler{
			isInteractive: true,
			confirmResult: true, // Confirm untrack
		}

		err := untrack.Action(s.Context, untrack.Options{
			BranchName: "feature",
		}, handler)
		require.NoError(t, err)
		require.True(t, handler.promptConfirmCalled)
		require.Equal(t, "feature", handler.lastBranchName)
		require.Equal(t, 1, handler.lastDescendantCount)
		require.False(t, s.Engine.GetBranch("feature").IsTracked())
		require.False(t, s.Engine.GetBranch("child").IsTracked())
	})

	t.Run("cancels when user declines confirmation", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit().
			CreateBranch("feature").
			Commit("feature commit").
			TrackBranch("feature", "main").
			CreateBranch("child").
			Commit("child commit").
			TrackBranch("child", "feature").
			Checkout("feature")

		handler := &testUntrackHandler{
			isInteractive: true,
			confirmResult: false, // Decline untrack
		}

		err := untrack.Action(s.Context, untrack.Options{
			BranchName: "feature",
		}, handler)
		require.NoError(t, err)
		require.True(t, handler.promptConfirmCalled)
		// Branches should still be tracked
		require.True(t, s.Engine.GetBranch("feature").IsTracked())
		require.True(t, s.Engine.GetBranch("child").IsTracked())
	})

	t.Run("nil handler cancels when descendants exist", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit().
			CreateBranch("feature").
			Commit("feature commit").
			TrackBranch("feature", "main").
			CreateBranch("child").
			Commit("child commit").
			TrackBranch("child", "feature").
			Checkout("feature")

		// nil handler uses NullHandler which returns false (cancel)
		err := untrack.Action(s.Context, untrack.Options{
			BranchName: "feature",
		}, nil)
		require.NoError(t, err)
		// Branches should still be tracked because NullHandler cancels
		require.True(t, s.Engine.GetBranch("feature").IsTracked())
		require.True(t, s.Engine.GetBranch("child").IsTracked())
	})

	t.Run("untracks multiple descendants", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit().
			CreateBranch("feature").
			Commit("feature commit").
			TrackBranch("feature", "main").
			CreateBranch("child1").
			Commit("child1 commit").
			TrackBranch("child1", "feature").
			CreateBranch("grandchild").
			Commit("grandchild commit").
			TrackBranch("grandchild", "child1").
			Checkout("feature").
			CreateBranch("child2").
			Commit("child2 commit").
			TrackBranch("child2", "feature").
			Checkout("feature")

		handler := &testUntrackHandler{
			isInteractive: true,
			confirmResult: true,
		}

		err := untrack.Action(s.Context, untrack.Options{
			BranchName: "feature",
		}, handler)
		require.NoError(t, err)
		require.True(t, handler.promptConfirmCalled)
		require.Equal(t, 3, handler.lastDescendantCount) // child1, child2, grandchild
		require.False(t, s.Engine.GetBranch("feature").IsTracked())
		require.False(t, s.Engine.GetBranch("child1").IsTracked())
		require.False(t, s.Engine.GetBranch("child2").IsTracked())
		require.False(t, s.Engine.GetBranch("grandchild").IsTracked())
	})
}
