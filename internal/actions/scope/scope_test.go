package scope_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/actions/scope"
	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

// testScopeHandler is a test handler for scope operations
type testScopeHandler struct {
	confirmRename      bool
	confirmErr         error
	isInteractive      bool
	promptRenameCalled bool
	lastOldScope       string
	lastNewScope       string
}

func (h *testScopeHandler) PromptConfirmRename(_, oldScope, newScope string) (bool, error) {
	h.promptRenameCalled = true
	h.lastOldScope = oldScope
	h.lastNewScope = newScope
	return h.confirmRename, h.confirmErr
}

func (h *testScopeHandler) Cleanup() {}

func (h *testScopeHandler) IsInteractive() bool { return h.isInteractive }

func TestScopeAction(t *testing.T) {
	t.Parallel()
	t.Run("show scope when no args provided", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit().
			CreateBranch("feature").
			Commit("feature commit").
			TrackBranch("feature", "main")

		err := scope.Action(s.Context, scope.Options{
			Show: true,
		}, nil)
		require.NoError(t, err)
	})

	t.Run("set scope on branch", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit().
			CreateBranch("feature").
			Commit("feature commit").
			TrackBranch("feature", "main")

		err := scope.Action(s.Context, scope.Options{
			Scope: "JIRA-123",
		}, nil)
		require.NoError(t, err)

		resolvedScope := s.Engine.GetScope(s.Engine.GetBranch("feature"))
		require.Equal(t, "JIRA-123", resolvedScope.String())
	})

	t.Run("unset scope on branch", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit().
			CreateBranch("feature").
			Commit("feature commit").
			TrackBranch("feature", "main")

		// First set a scope
		err := scope.Action(s.Context, scope.Options{
			Scope: "JIRA-123",
		}, nil)
		require.NoError(t, err)

		// Then unset it
		err = scope.Action(s.Context, scope.Options{
			Unset: true,
		}, nil)
		require.NoError(t, err)
	})

	t.Run("cannot set scope on trunk", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit()

		err := scope.Action(s.Context, scope.Options{
			Scope: "JIRA-123",
		}, nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "cannot set scope on trunk")
	})

	t.Run("PromptConfirmRename is called when scope changes and branch name contains old scope", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit().
			CreateBranch("JIRA-100-feature").
			Commit("feature commit").
			TrackBranch("JIRA-100-feature", "main")

		// Set initial scope
		err := scope.Action(s.Context, scope.Options{
			Scope: "JIRA-100",
		}, nil)
		require.NoError(t, err)

		// Change scope - should prompt for rename
		handler := &testScopeHandler{
			isInteractive: true,
			confirmRename: false, // Decline rename
		}
		err = scope.Action(s.Context, scope.Options{
			Scope: "JIRA-200",
		}, handler)
		require.NoError(t, err)
		require.True(t, handler.promptRenameCalled)
		require.Equal(t, "JIRA-100", handler.lastOldScope)
		require.Equal(t, "JIRA-200", handler.lastNewScope)
	})

	t.Run("PromptConfirmRename is not called when branch name does not contain old scope", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit().
			CreateBranch("feature").
			Commit("feature commit").
			TrackBranch("feature", "main")

		// Set initial scope
		err := scope.Action(s.Context, scope.Options{
			Scope: "JIRA-100",
		}, nil)
		require.NoError(t, err)

		// Change scope - should NOT prompt for rename because branch name doesn't contain old scope
		handler := &testScopeHandler{
			isInteractive: true,
			confirmRename: true,
		}
		err = scope.Action(s.Context, scope.Options{
			Scope: "JIRA-200",
		}, handler)
		require.NoError(t, err)
		require.False(t, handler.promptRenameCalled)
	})

	t.Run("non-interactive handler does not rename branch", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit().
			CreateBranch("JIRA-100-feature").
			Commit("feature commit").
			TrackBranch("JIRA-100-feature", "main")

		// Set initial scope
		err := scope.Action(s.Context, scope.Options{
			Scope: "JIRA-100",
		}, nil)
		require.NoError(t, err)

		// Change scope with non-interactive handler
		handler := &testScopeHandler{
			isInteractive: false,
		}
		err = scope.Action(s.Context, scope.Options{
			Scope: "JIRA-200",
		}, handler)
		require.NoError(t, err)
		// Branch should still have old name
		require.NotNil(t, s.Engine.GetBranch("JIRA-100-feature"))
	})
}
