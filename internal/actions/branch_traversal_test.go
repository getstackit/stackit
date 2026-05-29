package actions_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/actions/navigation"
	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

// testNavigationHandler is a test handler for navigation operations
type testNavigationHandler struct {
	branchToReturn     string
	selectErr          error
	isInteractive      bool
	promptSelectCalled bool
	lastMessage        string
	lastBranches       []string
}

func (h *testNavigationHandler) PromptSelectBranch(message string, branches []string) (string, error) {
	h.promptSelectCalled = true
	h.lastMessage = message
	h.lastBranches = branches
	return h.branchToReturn, h.selectErr
}

func (h *testNavigationHandler) Cleanup() {}

func (h *testNavigationHandler) IsInteractive() bool { return h.isInteractive }

func TestSwitchBranchAction(t *testing.T) {
	t.Parallel()
	t.Run("traverses downward to bottom branch", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
				"branch2": "branch1",
			})

		// Switch to branch2 (top of stack)
		s.Checkout("branch2")

		// Traverse downward should go to branch1 (first branch from trunk)
		_, err := navigation.SwitchBranchAction(navigation.DirectionBottom, s.Context, nil)
		require.NoError(t, err)

		// Should be on branch1
		currentBranch, err := s.Scene.Repo.CurrentBranchName()
		require.NoError(t, err)
		require.Equal(t, "branch1", currentBranch)
	})

	t.Run("traverses upward to top branch", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
				"branch2": "branch1",
			})

		// Switch to branch1
		s.Checkout("branch1")

		// Traverse upward should go to branch2 (top of stack)
		_, err := navigation.SwitchBranchAction(navigation.DirectionTop, s.Context, nil)
		require.NoError(t, err)

		// Should be on branch2
		currentBranch, err := s.Scene.Repo.CurrentBranchName()
		require.NoError(t, err)
		require.Equal(t, "branch2", currentBranch)
	})

	t.Run("returns error when not on a branch", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

		// Detach HEAD
		s.RunGit("checkout", "HEAD~0").Rebuild()

		_, err := navigation.SwitchBranchAction(navigation.DirectionBottom, s.Context, nil)
		require.Error(t, err)
	})

	t.Run("stays on branch when already at bottom", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
			})

		// Switch to branch1 (bottom of stack)
		s.Checkout("branch1")

		// Already on branch1 (bottom of stack)
		_, err := navigation.SwitchBranchAction(navigation.DirectionBottom, s.Context, nil)
		require.NoError(t, err)

		// Should still be on branch1
		currentBranch, err := s.Scene.Repo.CurrentBranchName()
		require.NoError(t, err)
		require.Equal(t, "branch1", currentBranch)
	})

	t.Run("stays on branch when already at top", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
			})

		// Switch to branch1 (top of stack)
		s.Checkout("branch1")

		// Already at top
		_, err := navigation.SwitchBranchAction(navigation.DirectionTop, s.Context, nil)
		require.NoError(t, err)

		// Should still be on branch1
		currentBranch, err := s.Scene.Repo.CurrentBranchName()
		require.NoError(t, err)
		require.Equal(t, "branch1", currentBranch)
	})

	t.Run("non-interactive mode with multiple children returns error", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
				"child1":  "branch1",
				"child2":  "branch1",
			})

		// Switch to branch1 (has two children)
		s.Checkout("branch1")

		// Non-interactive handler
		handler := &testNavigationHandler{
			isInteractive: false,
		}

		// Should return error because multiple children and non-interactive
		_, err := navigation.SwitchBranchAction(navigation.DirectionTop, s.Context, handler)
		require.Error(t, err)
		require.Contains(t, err.Error(), "multiple branches found")
		require.Contains(t, err.Error(), "non-interactive")
	})

	t.Run("interactive mode with multiple children prompts for selection", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
				"child1":  "branch1",
				"child2":  "branch1",
			})

		// Switch to branch1 (has two children)
		s.Checkout("branch1")

		// Interactive handler that selects child1
		handler := &testNavigationHandler{
			isInteractive:  true,
			branchToReturn: "child1",
		}

		_, err := navigation.SwitchBranchAction(navigation.DirectionTop, s.Context, handler)
		require.NoError(t, err)
		require.True(t, handler.promptSelectCalled)
		require.Contains(t, handler.lastBranches, "child1")
		require.Contains(t, handler.lastBranches, "child2")

		// Should be on child1
		currentBranch, err := s.Scene.Repo.CurrentBranchName()
		require.NoError(t, err)
		require.Equal(t, "child1", currentBranch)
	})

	t.Run("interactive mode follows selected branch recursively", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1":    "main",
				"child1":     "branch1",
				"child2":     "branch1",
				"grandchild": "child1",
			})

		// Switch to branch1 (has two children, one has grandchild)
		s.Checkout("branch1")

		// Interactive handler that selects child1 (which has grandchild)
		handler := &testNavigationHandler{
			isInteractive:  true,
			branchToReturn: "child1",
		}

		_, err := navigation.SwitchBranchAction(navigation.DirectionTop, s.Context, handler)
		require.NoError(t, err)
		require.True(t, handler.promptSelectCalled)

		// Should be on grandchild (the tip of the child1 branch)
		currentBranch, err := s.Scene.Repo.CurrentBranchName()
		require.NoError(t, err)
		require.Equal(t, "grandchild", currentBranch)
	})
}
