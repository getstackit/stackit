package sync

import (
	"testing"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/tui"
)

// viewString extracts the string content from a tea.View for test assertions.
func viewString(v tea.View) string {
	return v.Content
}

func TestNewModel(t *testing.T) {
	t.Parallel()
	model := NewModel(10)

	assert.Equal(t, 10, model.TotalOps)
	assert.Equal(t, 0, model.CompletedOps)
	assert.False(t, model.Done)
	assert.Equal(t, Phase(""), model.CurrentPhase)
}

func TestModel_Init(t *testing.T) {
	t.Parallel()
	model := NewModel(0)

	// Set up ready channel to capture signal
	readyChan := make(chan struct{})
	model.SetReadyChan(readyChan)

	cmd := model.Init()

	// Should signal ready
	select {
	case <-readyChan:
		// Ready signal received as expected
	default:
		t.Error("expected ready signal but channel was not closed")
	}

	// Should return a spinner tick command
	require.NotNil(t, cmd, "Init should return a command for spinner tick")
}

func TestModel_Update_PhaseStartMsg(t *testing.T) {
	t.Parallel()
	model := NewModel(10)
	model.Init()

	// Start trunk phase - returns a tea.Printf command
	newModel, cmd := model.Update(PhaseStartMsg{Phase: PhaseTrunk, Message: "📥 Pulling from remote..."})
	m := newModel.(*Model)

	assert.Equal(t, PhaseTrunk, m.CurrentPhase)
	assert.Equal(t, "", m.CurrentDetail) // Detail cleared on phase start
	assert.NotNil(t, cmd, "should return print command")
}

func TestModel_Update_PhaseTransition(t *testing.T) {
	t.Parallel()
	model := NewModel(10)
	model.Init()

	// Start trunk phase
	newModel, _ := model.Update(PhaseStartMsg{Phase: PhaseTrunk, Message: "📥 Pulling..."})
	m := newModel.(*Model)

	// Start github phase
	newModel, _ = m.Update(PhaseStartMsg{Phase: PhaseGitHub, Message: "🔄 Fetching..."})
	m = newModel.(*Model)

	assert.Equal(t, PhaseGitHub, m.CurrentPhase)
}

func TestModel_Update_PhaseDetailMsg(t *testing.T) {
	t.Parallel()
	model := NewModel(10)
	model.Init()

	// Start trunk phase first
	newModel, _ := model.Update(PhaseStartMsg{Phase: PhaseTrunk, Message: "📥 Pulling..."})
	m := newModel.(*Model)

	// Add a detail to trunk phase - returns a tea.Printf command
	newModel, cmd := m.Update(PhaseDetailMsg{
		Phase:   PhaseTrunk,
		Message: "main fast-forwarded to abc1234",
	})
	m = newModel.(*Model)

	assert.Equal(t, "main fast-forwarded to abc1234", m.CurrentDetail)
	assert.NotNil(t, cmd, "should return print command")
}

func TestModel_Update_PhaseDetailMsg_WithWarn(t *testing.T) {
	t.Parallel()
	model := NewModel(10)
	model.Init()

	// Add a detail with warning flag
	newModel, cmd := model.Update(PhaseDetailMsg{
		Phase:   PhaseTrunk,
		Message: "branch diverged",
		IsWarn:  true,
	})
	m := newModel.(*Model)

	assert.Equal(t, "branch diverged", m.CurrentDetail)
	assert.NotNil(t, cmd, "should return print command")
}

func TestModel_Update_ProgressTickMsg(t *testing.T) {
	t.Parallel()
	model := NewModel(10)
	model.Init()

	// Update progress
	newModel, cmd := model.Update(ProgressTickMsg{
		Completed: 5,
		Total:     10,
	})
	m := newModel.(*Model)

	assert.Equal(t, 5, m.CompletedOps)
	assert.Equal(t, 10, m.TotalOps)
	assert.NotNil(t, cmd, "should return progress update command")
}

func TestModel_Update_CompleteMsg(t *testing.T) {
	t.Parallel()
	model := NewModel(10)
	model.Init()

	// Send complete message
	newModel, cmd := model.Update(CompleteMsg{
		Summary: "All done!",
	})
	m := newModel.(*Model)

	assert.True(t, m.Done)
	assert.Equal(t, "All done!", m.Summary)

	// Should return a sequence command (print + quit)
	require.NotNil(t, cmd)
}

func TestModel_Update_KeyMsg_Quit(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		msg  tea.Msg
	}{
		{"ctrl+c quits", tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}},
		{"q quits", tea.KeyPressMsg{Code: 'q', Text: "q"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			model := NewModel(10)
			model.Init()

			_, cmd := model.Update(tt.msg)

			assert.NotNil(t, cmd, "should return quit command")
		})
	}
}

func TestModel_Update_WindowSizeMsg(t *testing.T) {
	t.Parallel()
	model := NewModel(10)
	model.Init()

	newModel, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 50})
	m := newModel.(*Model)

	assert.Equal(t, 100, m.Width)
	assert.Equal(t, 50, m.Height)
	// Progress width should be capped at 60
	assert.Equal(t, 60, m.Progress.Width())
}

func TestModel_Update_WindowSizeMsg_NarrowTerminal(t *testing.T) {
	t.Parallel()
	model := NewModel(10)
	model.Init()

	newModel, _ := model.Update(tea.WindowSizeMsg{Width: 50, Height: 30})
	m := newModel.(*Model)

	assert.Equal(t, 50, m.Width)
	assert.Equal(t, 30, m.Height)
	// Progress width should be 40 (50 - 10)
	assert.Equal(t, 40, m.Progress.Width())
}

func TestModel_View_InProgress(t *testing.T) {
	t.Parallel()
	model := NewModel(10)
	model.Init()

	// Start a phase
	newModel, _ := model.Update(PhaseStartMsg{Phase: PhaseTrunk, Message: "📥 Pulling..."})
	m := newModel.(*Model)

	// Update progress
	newModel, _ = m.Update(ProgressTickMsg{Completed: 3, Total: 10})
	m = newModel.(*Model)

	view := viewString(m.View())

	// Should contain progress info (package-manager style single line)
	assert.Contains(t, view, "3/10")
	// Should contain spinner
	assert.NotEmpty(t, view)
}

func TestModel_View_Completed(t *testing.T) {
	t.Parallel()
	model := NewModel(10)
	model.Init()

	// Complete the operation
	newModel, _ := model.Update(CompleteMsg{Summary: "Everything synced!"})
	m := newModel.(*Model)

	view := viewString(m.View())

	// View should be empty when done (summary printed via tea.Printf)
	assert.Empty(t, view)
}

func TestModel_Update_SpinnerTickMsg(t *testing.T) {
	t.Parallel()
	model := NewModel(10)
	model.Init()

	// Send a spinner tick
	_, cmd := model.Update(spinner.TickMsg{})

	// Should return another tick command to keep spinner animating
	assert.NotNil(t, cmd, "should return spinner tick command")
}

// TestMessageRecorder_Usage demonstrates how MessageRecorder can be used
// to test message flow in more complex scenarios
func TestMessageRecorder_Usage(t *testing.T) {
	t.Parallel()
	recorder := tui.NewMessageRecorder()

	// Record some messages
	recorder.Record(PhaseStartMsg{Phase: PhaseTrunk, Message: "📥 Pulling..."})
	recorder.Record(PhaseDetailMsg{Phase: PhaseTrunk, Message: "test"})
	recorder.Record(ProgressTickMsg{Completed: 1, Total: 5})

	assert.Equal(t, 3, recorder.Count())

	// Check for specific message types
	hasPhaseStart := recorder.HasMessage(func(msg tea.Msg) bool {
		_, ok := msg.(PhaseStartMsg)
		return ok
	})
	assert.True(t, hasPhaseStart)

	hasComplete := recorder.HasMessage(func(msg tea.Msg) bool {
		_, ok := msg.(CompleteMsg)
		return ok
	})
	assert.False(t, hasComplete)

	// Reset and verify
	recorder.Reset()
	assert.Equal(t, 0, recorder.Count())
}

func TestModel_GetStatusText(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		phase    Phase
		expected string
	}{
		{"trunk", PhaseTrunk, "Pulling from remote..."},
		{"branches", PhaseBranches, "Syncing branches..."},
		{"github", PhaseGitHub, "Fetching PR info..."},
		{"clean", PhaseClean, "Cleaning branches..."},
		{"restack", PhaseRestack, "Restacking branches..."},
		{"unknown", Phase(""), "Syncing..."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			model := NewModel(10)
			model.CurrentPhase = tt.phase
			text := model.getStatusText()
			assert.Equal(t, tt.expected, text)
		})
	}
}
