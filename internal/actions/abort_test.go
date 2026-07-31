package actions_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/actions/abort"
	"github.com/getstackit/stackit/internal/config"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

// testAbortHandler is a test handler for abort operations
type testAbortHandler struct {
	confirmResult       bool
	confirmErr          error
	isInteractive       bool
	promptConfirmCalled bool
}

func (h *testAbortHandler) PromptConfirmAbort() (bool, error) {
	h.promptConfirmCalled = true
	return h.confirmResult, h.confirmErr
}

func (h *testAbortHandler) Cleanup() {}

func (h *testAbortHandler) IsInteractive() bool { return h.isInteractive }

func TestAbortAction(t *testing.T) {
	t.Parallel()
	t.Run("reports when no operation is in progress", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit()

		err := abort.Action(s.Context, abort.Options{}, nil)
		require.NoError(t, err)
	})

	t.Run("aborts when continuation state exists", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit().
			CreateBranch("feature").
			Commit("feature change").
			Checkout("main").
			TrackBranch("feature", "main")

		// Get initial state
		initialSHA, err := s.Engine.GetBranch("feature").GetRevision()
		require.NoError(t, err)

		// Take snapshot
		err = s.Engine.TakeSnapshot(engine.SnapshotOptions{
			Command: "restack",
			Args:    []string{"feature"},
		})
		require.NoError(t, err)

		// Manually create continuation state
		continuationPath := filepath.Join(s.Scene.Dir, ".git", ".stackit_continue")
		err = os.WriteFile(continuationPath, []byte("{}"), 0644)
		require.NoError(t, err)

		// Run abort
		err = abort.Action(s.Context, abort.Options{Force: true}, nil)
		require.NoError(t, err)

		// Verify continuation state is gone
		_, err = os.Stat(continuationPath)
		require.True(t, os.IsNotExist(err), "continuation state should be gone")

		// Verify state restored
		s.Engine.Rebuild(s.Engine.Trunk().GetName())
		restoredSHA, err := s.Engine.GetBranch("feature").GetRevision()
		require.NoError(t, err)
		require.Equal(t, initialSHA, restoredSHA)
	})

	t.Run("warns when linear mode restores a fork", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit().
			CreateBranch("base").
			Commit("base change").
			CreateBranch("first").
			Commit("first change").
			Checkout("base").
			CreateBranch("second").
			Commit("second change").
			Checkout("main").
			TrackBranch("base", "main").
			TrackBranch("first", "base").
			TrackBranch("second", "base")

		cfg, err := config.LoadConfig(s.Scene.Dir)
		require.NoError(t, err)
		require.NoError(t, cfg.SetStackShape(config.StackShapeLinear))
		s.Context.Config = cfg

		require.NoError(t, s.Engine.TakeSnapshot(engine.SnapshotOptions{Command: "restack"}))
		require.NoError(t, s.Engine.SetParent(s.Context, s.Engine.GetBranch("second"), s.Engine.GetBranch("main"), engine.DivergenceRecompute))

		continuationPath := filepath.Join(s.Scene.Dir, ".git", ".stackit_continue")
		require.NoError(t, os.WriteFile(continuationPath, []byte("{}"), 0644))

		err = abort.Action(s.Context, abort.Options{Force: true}, nil)
		require.NoError(t, err)
		require.Contains(t, s.Output.String(), "Abort restored a non-linear stack")
	})

	t.Run("non-interactive handler without force does not abort", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit().
			CreateBranch("feature").
			Commit("feature change").
			TrackBranch("feature", "main")

		// Manually create continuation state
		continuationPath := filepath.Join(s.Scene.Dir, ".git", ".stackit_continue")
		err := os.WriteFile(continuationPath, []byte("{}"), 0644)
		require.NoError(t, err)

		// Use non-interactive handler that returns false
		handler := &testAbortHandler{
			isInteractive: false,
			confirmResult: false,
		}

		err = abort.Action(s.Context, abort.Options{}, handler)
		require.NoError(t, err)
		require.True(t, handler.promptConfirmCalled)

		// Continuation state should still exist because abort was canceled
		_, err = os.Stat(continuationPath)
		require.NoError(t, err, "continuation state should still exist")
	})

	t.Run("interactive handler with confirmation proceeds with abort", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit().
			CreateBranch("feature").
			Commit("feature change").
			TrackBranch("feature", "main")

		// Take snapshot
		err := s.Engine.TakeSnapshot(engine.SnapshotOptions{
			Command: "restack",
			Args:    []string{"feature"},
		})
		require.NoError(t, err)

		// Manually create continuation state
		continuationPath := filepath.Join(s.Scene.Dir, ".git", ".stackit_continue")
		err = os.WriteFile(continuationPath, []byte("{}"), 0644)
		require.NoError(t, err)

		// Use interactive handler that confirms
		handler := &testAbortHandler{
			isInteractive: true,
			confirmResult: true,
		}

		err = abort.Action(s.Context, abort.Options{}, handler)
		require.NoError(t, err)
		require.True(t, handler.promptConfirmCalled)

		// Continuation state should be gone
		_, err = os.Stat(continuationPath)
		require.True(t, os.IsNotExist(err), "continuation state should be gone")
	})

	t.Run("interactive handler with decline cancels abort", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit().
			CreateBranch("feature").
			Commit("feature change").
			TrackBranch("feature", "main")

		// Manually create continuation state
		continuationPath := filepath.Join(s.Scene.Dir, ".git", ".stackit_continue")
		err := os.WriteFile(continuationPath, []byte("{}"), 0644)
		require.NoError(t, err)

		// Use interactive handler that declines
		handler := &testAbortHandler{
			isInteractive: true,
			confirmResult: false,
		}

		err = abort.Action(s.Context, abort.Options{}, handler)
		require.NoError(t, err)
		require.True(t, handler.promptConfirmCalled)

		// Continuation state should still exist
		_, err = os.Stat(continuationPath)
		require.NoError(t, err, "continuation state should still exist")
	})

	t.Run("force flag bypasses handler prompt", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit().
			CreateBranch("feature").
			Commit("feature change").
			TrackBranch("feature", "main")

		// Take snapshot
		err := s.Engine.TakeSnapshot(engine.SnapshotOptions{
			Command: "restack",
			Args:    []string{"feature"},
		})
		require.NoError(t, err)

		// Manually create continuation state
		continuationPath := filepath.Join(s.Scene.Dir, ".git", ".stackit_continue")
		err = os.WriteFile(continuationPath, []byte("{}"), 0644)
		require.NoError(t, err)

		// Use handler that would decline, but force bypasses it
		handler := &testAbortHandler{
			isInteractive: true,
			confirmResult: false,
		}

		err = abort.Action(s.Context, abort.Options{Force: true}, handler)
		require.NoError(t, err)
		require.False(t, handler.promptConfirmCalled) // Force bypasses prompt

		// Continuation state should be gone
		_, err = os.Stat(continuationPath)
		require.True(t, os.IsNotExist(err), "continuation state should be gone")
	})
}
