package track_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/actions/track"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/github"
	"github.com/getstackit/stackit/internal/output"
	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

// testTrackHandler is a test handler for track operations
type testTrackHandler struct {
	parentToReturn        string
	parentErr             error
	trackChildResult      bool
	trackChildErr         error
	isInteractive         bool
	promptSelectCalled    bool
	promptTrackChildCalls []string
}

func (h *testTrackHandler) PromptSelectParent(_ context.Context, _ engine.Engine, _ github.Client, _ output.Logger, _ string) (string, error) {
	h.promptSelectCalled = true
	return h.parentToReturn, h.parentErr
}

func (h *testTrackHandler) PromptTrackChild(childName, _ string) (bool, error) {
	h.promptTrackChildCalls = append(h.promptTrackChildCalls, childName)
	return h.trackChildResult, h.trackChildErr
}

func (h *testTrackHandler) Cleanup() {}

func (h *testTrackHandler) IsInteractive() bool { return h.isInteractive }

func TestTrackAction(t *testing.T) {
	t.Run("track with --parent flag validates parent exists", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit().
			CreateBranchQuiet("feature")

		err := track.Action(s.Context, track.Options{
			BranchName: "feature",
			Parent:     "nonexistent",
		}, nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "does not exist")
	})

	t.Run("track with --parent flag validates parent is tracked", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit().
			CreateBranchQuiet("untracked-parent").
			Checkout("main").
			CreateBranchQuiet("feature")

		err := track.Action(s.Context, track.Options{
			BranchName: "feature",
			Parent:     "untracked-parent",
		}, nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "must be tracked")
	})

	t.Run("track with --force succeeds using trunk as ancestor", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit().
			CreateBranchQuiet("feature")

		// With --force, auto-detects trunk as the ancestor
		err := track.Action(s.Context, track.Options{
			BranchName: "feature",
			Force:      true,
		}, nil)
		require.NoError(t, err)
		require.True(t, s.Engine.GetBranch("feature").IsTracked())
		require.Equal(t, "main", s.Engine.GetBranch("feature").GetParent().GetName())
	})

	t.Run("non-interactive mode requires --parent or --force", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit().
			CreateBranchQuiet("feature")

		handler := &testTrackHandler{isInteractive: false}
		err := track.Action(s.Context, track.Options{
			BranchName: "feature",
		}, handler)
		require.Error(t, err)
		require.Contains(t, err.Error(), "non-interactive mode")
	})

	t.Run("interactive mode calls PromptSelectParent when auto-detection fails", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit().
			CreateBranchQuiet("feature")

		handler := &testTrackHandler{
			isInteractive:  true,
			parentToReturn: "main",
		}
		err := track.Action(s.Context, track.Options{
			BranchName: "feature",
		}, handler)
		require.NoError(t, err)
		require.True(t, handler.promptSelectCalled)
		require.True(t, s.Engine.GetBranch("feature").IsTracked())
	})

	t.Run("interactive mode handles user cancellation (empty parent)", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit().
			CreateBranchQuiet("feature")

		handler := &testTrackHandler{
			isInteractive:  true,
			parentToReturn: "", // User cancels
		}
		err := track.Action(s.Context, track.Options{
			BranchName: "feature",
		}, handler)
		require.NoError(t, err)
		require.True(t, handler.promptSelectCalled)
		// Branch should NOT be tracked because user canceled
		require.False(t, s.Engine.GetBranch("feature").IsTracked())
	})

	t.Run("interactive mode auto-detects parent when unambiguous", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit().
			CreateBranch("base").
			Commit("base commit").
			TrackBranch("base", "main").
			CreateBranchQuiet("feature")

		handler := &testTrackHandler{
			isInteractive: true,
		}
		err := track.Action(s.Context, track.Options{
			BranchName: "feature",
		}, handler)
		require.NoError(t, err)
		// Should NOT have called PromptSelectParent because auto-detection worked
		require.False(t, handler.promptSelectCalled)
		require.True(t, s.Engine.GetBranch("feature").IsTracked())
		require.Equal(t, "base", s.Engine.GetBranch("feature").GetParent().GetName())
	})

	t.Run("PromptTrackChild is called for untracked children", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit().
			CreateBranchQuiet("feature").
			Commit("feature commit").
			CreateBranchQuiet("child1").
			Commit("child commit").
			Checkout("feature").
			Rebuild()

		// Ensure child1 is NOT tracked (just created with git, not stackit)
		require.False(t, s.Engine.GetBranch("child1").IsTracked())

		handler := &testTrackHandler{
			isInteractive:    true,
			parentToReturn:   "main",
			trackChildResult: false, // Decline tracking children
		}
		err := track.Action(s.Context, track.Options{
			BranchName: "feature",
		}, handler)
		require.NoError(t, err)
		require.True(t, s.Engine.GetBranch("feature").IsTracked())
		// Child should have been offered for tracking
		require.Len(t, handler.promptTrackChildCalls, 1)
		require.Equal(t, "child1", handler.promptTrackChildCalls[0])
	})
}
