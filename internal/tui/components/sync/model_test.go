package sync

import (
	"testing"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
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
	model := NewModel()

	assert.False(t, model.Done)
	assert.Equal(t, Phase(""), model.CurrentPhase)
}

func TestModel_Init(t *testing.T) {
	t.Parallel()
	model := NewModel()

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

func TestModel_View_ShowsPhaseStepAndElapsed(t *testing.T) {
	t.Parallel()
	model := NewModel()
	model.Init()

	// The step comes from the fixed pipeline, so it is known before any work
	// happens — no denominator can be wrong and none can be missing.
	newModel, _ := model.Update(PhaseStartMsg{Phase: PhaseRestack})
	m := newModel.(*Model)

	view := viewString(m.View())
	assert.Contains(t, ansi.Strip(view), "●●●●● Restacking branches...")
	assert.Regexp(t, `\d+\.\ds`, view, "elapsed clock is always shown")
}

func TestModel_Update_PrintMsg(t *testing.T) {
	t.Parallel()
	model := NewModel()
	model.Init()

	// The handler composes finished lines; the model only prints them.
	_, cmd := model.Update(PrintMsg{Lines: []string{"", "🧹 2 branches landed and cleaned up", "  #1230 feat-a"}})
	assert.NotNil(t, cmd, "should return print commands")
}

func TestModel_Update_ActivityMsg_NamesWorkOnLiveLine(t *testing.T) {
	t.Parallel()
	model := NewModel()
	model.Init()

	newModel, _ := model.Update(PhaseStartMsg{Phase: PhaseGitHub})
	m := newModel.(*Model)
	newModel, cmd := m.Update(ActivityMsg{Text: "Updating PR for feat/api"})
	m = newModel.(*Model)

	// Activity is live-line only: it is superseded by the phase's own report.
	assert.Nil(t, cmd, "activity must not print to scrollback")
	assert.Contains(t, ansi.Strip(viewString(m.View())), "Updating PR for feat/api")

	// A new phase clears it.
	newModel, _ = m.Update(PhaseStartMsg{Phase: PhaseRestack})
	m = newModel.(*Model)
	assert.Contains(t, ansi.Strip(viewString(m.View())), "Restacking branches...")
}

func TestModel_Update_PhaseTransition(t *testing.T) {
	t.Parallel()
	model := NewModel()
	model.Init()

	// Start trunk phase
	newModel, _ := model.Update(PhaseStartMsg{Phase: PhaseTrunk})
	m := newModel.(*Model)

	// Start github phase
	newModel, _ = m.Update(PhaseStartMsg{Phase: PhaseGitHub})
	m = newModel.(*Model)

	assert.Equal(t, PhaseGitHub, m.CurrentPhase)
}

func TestModel_Update_CompleteMsg(t *testing.T) {
	t.Parallel()
	model := NewModel()
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
			model := NewModel()
			model.Init()

			_, cmd := model.Update(tt.msg)

			assert.NotNil(t, cmd, "should return quit command")
		})
	}
}

func TestModel_Update_WindowSizeMsg(t *testing.T) {
	t.Parallel()
	model := NewModel()
	model.Init()

	newModel, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 50})
	m := newModel.(*Model)

	assert.Equal(t, 100, m.Width)
	assert.Equal(t, 50, m.Height)
}

func TestModel_Update_WindowSizeMsg_NarrowTerminal(t *testing.T) {
	t.Parallel()
	model := NewModel()
	model.Init()

	newModel, _ := model.Update(tea.WindowSizeMsg{Width: 50, Height: 30})
	m := newModel.(*Model)

	assert.Equal(t, 50, m.Width)
	assert.Equal(t, 30, m.Height)
}

func TestModel_View_InProgress(t *testing.T) {
	t.Parallel()
	model := NewModel()
	model.Init()

	// Start a phase
	newModel, _ := model.Update(PhaseStartMsg{Phase: PhaseTrunk})
	m := newModel.(*Model)

	view := viewString(m.View())

	// One line: spinner, phase step, elapsed clock.
	assert.Contains(t, ansi.Strip(view), "●○○○○ Pulling from remote...")
	assert.NotEmpty(t, view)
}

func TestModel_View_Completed(t *testing.T) {
	t.Parallel()
	model := NewModel()
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
	model := NewModel()
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
	recorder.Record(PhaseStartMsg{Phase: PhaseTrunk})
	recorder.Record(PrintMsg{Lines: []string{"first"}})
	recorder.Record(ActivityMsg{Text: "second"})

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
		{"trunk", PhaseTrunk, "●○○○○ Pulling from remote..."},
		{"github", PhaseGitHub, "●●○○○ Fetching PR info..."},
		{"branches", PhaseBranches, "●●●○○ Syncing branches..."},
		{"clean", PhaseClean, "●●●●○ Cleaning branches..."},
		{"restack", PhaseRestack, "●●●●● Restacking branches..."},
		{"unknown", Phase(""), "Syncing..."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			model := NewModel()
			model.CurrentPhase = tt.phase
			// Strip color so the assertion is about the pip pattern, not the styling.
			assert.Equal(t, tt.expected, ansi.Strip(model.getStatusText()))
		})
	}
}
