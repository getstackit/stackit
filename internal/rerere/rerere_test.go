package rerere

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/testhelpers"
)

func TestEnsureEnabled(t *testing.T) {
	t.Parallel()

	t.Run("non-interactive skips prompt", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)
		runner := git.NewRunnerWithPath(scene.Dir, nil)

		prompted := false
		confirm := func(string, bool) (bool, error) {
			prompted = true
			return true, nil
		}

		enabled, err := ensureEnabled(context.Background(), runner, false, nil, confirm)
		require.NoError(t, err)
		require.False(t, enabled)
		require.False(t, prompted)
	})

	t.Run("accepted prompt enables rerere", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)
		runner := git.NewRunnerWithPath(scene.Dir, nil)

		confirm := func(prompt string, defaultYes bool) (bool, error) {
			require.Equal(t, "Enable git rerere to remember conflict resolutions?", prompt)
			require.True(t, defaultYes)
			return true, nil
		}

		enabled, err := ensureEnabled(context.Background(), runner, true, nil, confirm)
		require.NoError(t, err)
		require.True(t, enabled)

		rerereEnabled, err := runner.GetConfig("rerere.enabled")
		require.NoError(t, err)
		require.Equal(t, "true", rerereEnabled)

		rerereAutoupdate, err := runner.GetConfig("rerere.autoupdate")
		require.NoError(t, err)
		require.Equal(t, "true", rerereAutoupdate)
	})

	t.Run("declined prompt is remembered", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)
		runner := git.NewRunnerWithPath(scene.Dir, nil)

		confirm := func(string, bool) (bool, error) {
			return false, nil
		}

		enabled, err := ensureEnabled(context.Background(), runner, true, nil, confirm)
		require.NoError(t, err)
		require.False(t, enabled)

		declined, err := runner.GetConfig(declinedKey)
		require.NoError(t, err)
		require.Equal(t, "true", declined)

		promptedAgain := false
		confirm = func(string, bool) (bool, error) {
			promptedAgain = true
			return true, nil
		}

		enabled, err = ensureEnabled(context.Background(), runner, true, nil, confirm)
		require.NoError(t, err)
		require.False(t, enabled)
		require.False(t, promptedAgain)
	})

	t.Run("already enabled skips prompt and sets autoupdate", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)
		runner := git.NewRunnerWithPath(scene.Dir, nil)
		require.NoError(t, runner.SetConfig("rerere.enabled", "true"))

		prompted := false
		confirm := func(string, bool) (bool, error) {
			prompted = true
			return true, nil
		}

		enabled, err := ensureEnabled(context.Background(), runner, true, nil, confirm)
		require.NoError(t, err)
		require.False(t, enabled)
		require.False(t, prompted)

		// rerere.autoupdate must be set — the auto-continue loop in
		// internal/git/rebase.go bails out when rerere applies a resolution
		// without staging it.
		autoupdate, err := runner.GetConfig("rerere.autoupdate")
		require.NoError(t, err)
		require.Equal(t, "true", autoupdate)
	})

	t.Run("already enabled with autoupdate=false is upgraded", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)
		runner := git.NewRunnerWithPath(scene.Dir, nil)
		require.NoError(t, runner.SetConfig("rerere.enabled", "true"))
		require.NoError(t, runner.SetConfig("rerere.autoupdate", "false"))

		confirm := func(string, bool) (bool, error) {
			t.Fatalf("prompt should not fire when rerere is already enabled")
			return false, nil
		}

		enabled, err := ensureEnabled(context.Background(), runner, true, nil, confirm)
		require.NoError(t, err)
		require.False(t, enabled)

		autoupdate, err := runner.GetConfig("rerere.autoupdate")
		require.NoError(t, err)
		require.Equal(t, "true", autoupdate)
	})

	t.Run("pauser wraps prompt", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)
		runner := git.NewRunnerWithPath(scene.Dir, nil)

		p := &recordingPauser{}
		confirm := func(string, bool) (bool, error) {
			require.Equal(t, 1, p.pauseCount, "Pause must run before prompt")
			require.Equal(t, 0, p.resumeCount, "Resume must run after prompt")
			return false, nil
		}

		_, err := ensureEnabled(context.Background(), runner, true, p, confirm)
		require.NoError(t, err)
		require.Equal(t, 1, p.pauseCount)
		require.Equal(t, 1, p.resumeCount)
	})
}

type recordingPauser struct {
	pauseCount  int
	resumeCount int
}

func (p *recordingPauser) Pause()  { p.pauseCount++ }
func (p *recordingPauser) Resume() { p.resumeCount++ }
