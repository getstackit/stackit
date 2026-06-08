package merge

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// viewString extracts the string content from a tea.View for test assertions.
func viewString(v tea.View) string {
	return v.Content
}

func TestModel_EmptyPlan(t *testing.T) {
	t.Parallel()
	t.Run("empty plan shows initializing message", func(t *testing.T) {
		t.Parallel()
		model := NewModel()

		// Before any plan is loaded, groups is empty
		require.Empty(t, model.Groups)
		require.Empty(t, model.Steps)

		view := viewString(model.View())

		// Should show "Merge Progress" header
		assert.Contains(t, view, "Merge Progress")

		// Should show initializing state, not "Step 1 of 0"
		assert.NotContains(t, view, "Step 1 of 0")
		assert.NotContains(t, view, "Step 0 of 0")
		assert.Contains(t, view, "Initializing...")
	})

	t.Run("empty plan with done shows complete", func(t *testing.T) {
		t.Parallel()
		model := NewModel()
		model.Done = true

		view := viewString(model.View())

		assert.Contains(t, view, "Merge Progress")
		assert.Contains(t, view, "Complete")
		assert.NotContains(t, view, "Step 1 of 0")
		assert.NotContains(t, view, "Initializing")
	})

	t.Run("empty plan with summary shows summary", func(t *testing.T) {
		t.Parallel()
		model := NewModel()
		model.Done = true
		model.Summary = "Created PR #123"

		view := viewString(model.View())

		assert.Contains(t, view, "Created PR #123")
	})
}

func TestModel_PlanLoadedMsg(t *testing.T) {
	t.Parallel()
	t.Run("loads plan with groups", func(t *testing.T) {
		t.Parallel()
		model := NewModel()

		msg := PlanLoadedMsg{
			Groups: []Group{
				{Label: "Group 1", StepIndices: []int{0}},
				{Label: "Group 2", StepIndices: []int{1, 2}},
			},
			StepDescriptions: []string{"Step 0", "Step 1", "Step 2"},
		}

		updated, _ := model.Update(msg)
		model = updated.(*Model)

		require.Len(t, model.Groups, 2)
		require.Len(t, model.Steps, 3)
		assert.Equal(t, "Group 1", model.Groups[0].Label)
		assert.Equal(t, "Group 2", model.Groups[1].Label)
	})

	t.Run("creates default groups when none provided", func(t *testing.T) {
		t.Parallel()
		model := NewModel()

		msg := PlanLoadedMsg{
			Groups:           []Group{},
			StepDescriptions: []string{"Step A", "Step B"},
		}

		updated, _ := model.Update(msg)
		model = updated.(*Model)

		// Should create one group per step
		require.Len(t, model.Groups, 2)
		assert.Equal(t, "Step A", model.Groups[0].Label)
		assert.Equal(t, "Step B", model.Groups[1].Label)
		assert.Equal(t, []int{0}, model.Groups[0].StepIndices)
		assert.Equal(t, []int{1}, model.Groups[1].StepIndices)
	})
}

func TestModel_View_ProgressIndicator(t *testing.T) {
	t.Parallel()
	t.Run("shows correct progress", func(t *testing.T) {
		t.Parallel()
		model := NewModel()

		// Load a plan with 3 groups
		loadMsg := PlanLoadedMsg{
			Groups: []Group{
				{Label: "Group 1", StepIndices: []int{0}},
				{Label: "Group 2", StepIndices: []int{1}},
				{Label: "Group 3", StepIndices: []int{2}},
			},
			StepDescriptions: []string{"Step 0", "Step 1", "Step 2"},
		}
		updated, _ := model.Update(loadMsg)
		model = updated.(*Model)

		view := viewString(model.View())
		assert.Contains(t, view, "Step 1 of 3")

		// Complete first step
		model.Steps[0].Status = StatusDone

		view = viewString(model.View())
		assert.Contains(t, view, "Step 2 of 3")

		// Complete second step
		model.Steps[1].Status = StatusDone

		view = viewString(model.View())
		assert.Contains(t, view, "Step 3 of 3")

		// Complete all steps and mark done
		model.Steps[2].Status = StatusDone
		model.Done = true

		view = viewString(model.View())
		assert.Contains(t, view, "3 of 3 complete")
	})
}

func TestModel_View_StepStatuses(t *testing.T) {
	t.Parallel()
	t.Run("shows running step with spinner", func(t *testing.T) {
		t.Parallel()
		model := NewModel()

		loadMsg := PlanLoadedMsg{
			Groups: []Group{
				{Label: "Merge PR #1", StepIndices: []int{0}},
			},
			StepDescriptions: []string{"Merge PR #1"},
		}
		updated, _ := model.Update(loadMsg)
		model = updated.(*Model)

		// Start the step
		startMsg := StepStartMsg{StepIndex: 0, Description: "Merge PR #1"}
		updated, _ = model.Update(startMsg)
		model = updated.(*Model)

		view := viewString(model.View())
		// Check that the group label is shown (bold styling stripped)
		assert.True(t, strings.Contains(view, "Merge PR #1"))
	})

	t.Run("shows completed step with checkmark", func(t *testing.T) {
		t.Parallel()
		model := NewModel()

		loadMsg := PlanLoadedMsg{
			Groups: []Group{
				{Label: "Merge PR #1", StepIndices: []int{0}},
			},
			StepDescriptions: []string{"Merge PR #1"},
		}
		updated, _ := model.Update(loadMsg)
		model = updated.(*Model)

		// Complete the step
		completeMsg := StepCompleteMsg{StepIndex: 0}
		updated, _ = model.Update(completeMsg)
		model = updated.(*Model)

		view := viewString(model.View())
		assert.Contains(t, view, "✓")
	})

	t.Run("shows failed step with error mark", func(t *testing.T) {
		t.Parallel()
		model := NewModel()

		loadMsg := PlanLoadedMsg{
			Groups: []Group{
				{Label: "Merge PR #1", StepIndices: []int{0}},
			},
			StepDescriptions: []string{"Merge PR #1"},
		}
		updated, _ := model.Update(loadMsg)
		model = updated.(*Model)

		// Fail the step
		failMsg := StepFailedMsg{StepIndex: 0, Error: assert.AnError}
		updated, _ = model.Update(failMsg)
		model = updated.(*Model)

		view := viewString(model.View())
		assert.Contains(t, view, "✗")
	})
}

func TestModel_Quitting(t *testing.T) {
	t.Parallel()
	t.Run("returns empty view when quitting", func(t *testing.T) {
		t.Parallel()
		model := NewModel()
		model.Quitting = true

		view := viewString(model.View())
		assert.Empty(t, view)
	})
}
