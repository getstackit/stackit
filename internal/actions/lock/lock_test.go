package lock_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/actions/lock"
	"github.com/getstackit/stackit/internal/actions/submit"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/output"
	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

// testLockHandler is a test handler for lock operations
type testLockHandler struct {
	submitResult        bool
	unlockDownstack     bool
	isInteractive       bool
	promptSubmitCalled  bool
	promptUnlockCalled  bool
	expectedPromptCheck func(string)
}

func (h *testLockHandler) PromptSubmitBeforeLock(_ []string) (bool, error) {
	h.promptSubmitCalled = true
	return h.submitResult, nil
}

func (h *testLockHandler) PromptUnlockDownstack(names []string) (bool, error) {
	h.promptUnlockCalled = true
	if h.expectedPromptCheck != nil && len(names) > 0 {
		h.expectedPromptCheck(names[0])
	}
	return h.unlockDownstack, nil
}

func (h *testLockHandler) GetSubmitHandler() submit.Handler { return nil }
func (h *testLockHandler) Cleanup()                         {}
func (h *testLockHandler) IsInteractive() bool              { return h.isInteractive }

func TestLockUnlockAction(t *testing.T) {
	t.Parallel()
	t.Run("LockAction locks branch and ancestors", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit().
			CreateBranch("feature-a").
			Commit("a1").
			CreateBranch("feature-b").
			Commit("b1").
			TrackBranch("feature-a", "main").
			TrackBranch("feature-b", "feature-a")

		err := lock.Action(s.Context, "feature-b", nil)
		require.NoError(t, err)

		require.True(t, s.Engine.GetBranch("feature-b").IsLocked())
		require.True(t, s.Engine.GetBranch("feature-a").IsLocked())
		require.Equal(t, engine.LockReasonUser, s.Engine.GetBranch("feature-b").GetLockReason())
		require.False(t, s.Engine.GetBranch("main").IsLocked())
	})

	t.Run("LockAction indicates already locked branches", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit().
			CreateBranch("feature-a").
			Commit("a1").
			TrackBranch("feature-a", "main")

		// Pre-lock
		_, err := s.Engine.SetLocked(context.Background(), engine.BranchesOf(s.Engine.GetBranch("feature-a")), engine.LockReasonUser)
		require.NoError(t, err)

		var buf bytes.Buffer
		s.Context.Output = output.NewConsoleOutput(&buf, false)

		err = lock.Action(s.Context, "feature-a", nil)
		require.NoError(t, err)

		out := buf.String()
		require.Contains(t, out, "feature-a")
		require.Contains(t, out, "already locked")
	})

	t.Run("UnlockAction unlocks branch and descendants", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit().
			CreateBranch("feature-a").
			Commit("a1").
			CreateBranch("feature-b").
			Commit("b1").
			TrackBranch("feature-a", "main").
			TrackBranch("feature-b", "feature-a")

		// Pre-lock
		_, err := s.Engine.SetLocked(context.Background(), engine.BranchesOf(s.Engine.GetBranch("feature-a")), engine.LockReasonUser)
		require.NoError(t, err)
		_, err = s.Engine.SetLocked(context.Background(), engine.BranchesOf(s.Engine.GetBranch("feature-b")), engine.LockReasonUser)
		require.NoError(t, err)

		err = lock.Unlock(s.Context, "feature-a", nil)
		require.NoError(t, err)

		require.False(t, s.Engine.GetBranch("feature-a").IsLocked())
		require.False(t, s.Engine.GetBranch("feature-b").IsLocked())
	})

	t.Run("UnlockAction indicates already unlocked branches", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit().
			CreateBranch("feature-a").
			Commit("a1").
			TrackBranch("feature-a", "main")

		// Already unlocked by default

		var buf bytes.Buffer
		s.Context.Output = output.NewConsoleOutput(&buf, false)

		err := lock.Unlock(s.Context, "feature-a", nil)
		require.NoError(t, err)

		out := buf.String()
		require.Contains(t, out, "feature-a")
		require.Contains(t, out, "already unlocked")
	})

	t.Run("LockAction fails on untracked branch", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit().
			CreateBranchQuiet("untracked")

		err := lock.Action(s.Context, "untracked", nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "not tracked")
	})

	t.Run("LockAction fails on trunk", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit()

		err := lock.Action(s.Context, "main", nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "cannot lock trunk")
	})

	t.Run("LockAction with unpushed commits in non-interactive mode", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit().
			CreateBranch("feature-a").
			Commit("a1").
			TrackBranch("feature-a", "main")

		// Create a remote
		_, err := s.Scene.Repo.CreateBareRemote("origin")
		require.NoError(t, err)

		// Push main but not feature-a
		err = s.Scene.Repo.PushBranch("origin", "main")
		require.NoError(t, err)

		// feature-a is now "ahead" of remote (remote doesn't have it)
		// Or if we pushed an earlier version, it would be ahead.
		// Let's push an earlier version.
		err = s.Scene.Repo.PushBranch("origin", "feature-a")
		require.NoError(t, err)
		s.Commit("a2") // feature-a is now ahead

		s.Context.Interactive = false // Ensure non-interactive

		err = lock.Action(s.Context, "feature-a", nil)
		require.NoError(t, err)
		require.True(t, s.Engine.GetBranch("feature-a").IsLocked())
	})

	t.Run("LockAction prompts for new branch not on remote", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit().
			CreateBranch("new-feature").
			Commit("new change").
			TrackBranch("new-feature", "main")

		// Create a bare remote but don't push new-feature
		_, err := s.Scene.Repo.CreateBareRemote("origin")
		require.NoError(t, err)

		// Use test handler that declines submit
		handler := &testLockHandler{
			isInteractive: true,
			submitResult:  false, // decline submit
		}

		err = lock.Action(s.Context, "new-feature", handler)
		require.NoError(t, err)
		require.True(t, s.Engine.GetBranch("new-feature").IsLocked())
		require.True(t, handler.promptSubmitCalled)
	})

	t.Run("UnlockAction prompts for downstack and unlocks it if confirmed", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit().
			CreateBranch("feature-a").
			Commit("a1").
			CreateBranch("feature-b").
			Commit("b1").
			TrackBranch("feature-a", "main").
			TrackBranch("feature-b", "feature-a")

		// Pre-lock both
		_, err := s.Engine.SetLocked(context.Background(), engine.BranchesOf(s.Engine.GetBranch("feature-a")), engine.LockReasonUser)
		require.NoError(t, err)
		_, err = s.Engine.SetLocked(context.Background(), engine.BranchesOf(s.Engine.GetBranch("feature-b")), engine.LockReasonUser)
		require.NoError(t, err)

		// Use test handler that confirms unlock
		handler := &testLockHandler{
			isInteractive:   true,
			unlockDownstack: true, // confirm unlock
			expectedPromptCheck: func(name string) {
				require.Equal(t, "feature-a", name)
			},
		}

		err = lock.Unlock(s.Context, "feature-b", handler)
		require.NoError(t, err)

		require.False(t, s.Engine.GetBranch("feature-b").IsLocked())
		require.False(t, s.Engine.GetBranch("feature-a").IsLocked())
		require.True(t, handler.promptUnlockCalled)
	})

	t.Run("UnlockAction prompts for downstack and does not unlock it if declined", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit().
			CreateBranch("feature-a").
			Commit("a1").
			CreateBranch("feature-b").
			Commit("b1").
			TrackBranch("feature-a", "main").
			TrackBranch("feature-b", "feature-a")

		// Pre-lock both
		_, err := s.Engine.SetLocked(context.Background(), engine.BranchesOf(s.Engine.GetBranch("feature-a")), engine.LockReasonUser)
		require.NoError(t, err)
		_, err = s.Engine.SetLocked(context.Background(), engine.BranchesOf(s.Engine.GetBranch("feature-b")), engine.LockReasonUser)
		require.NoError(t, err)

		// Use test handler that declines unlock
		handler := &testLockHandler{
			isInteractive:   true,
			unlockDownstack: false, // decline unlock
			expectedPromptCheck: func(name string) {
				require.Equal(t, "feature-a", name)
			},
		}

		err = lock.Unlock(s.Context, "feature-b", handler)
		require.NoError(t, err)

		require.False(t, s.Engine.GetBranch("feature-b").IsLocked())
		require.True(t, s.Engine.GetBranch("feature-a").IsLocked())
		require.True(t, handler.promptUnlockCalled)
	})
}
