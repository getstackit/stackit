package split

import (
	"testing"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
)

func TestModel_Update_KeyMsg_Quit_MarksCanceled(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		msg  tea.Msg
	}{
		{"ctrl+c cancels", tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}},
		{"q cancels", tea.KeyPressMsg{Code: 'q', Text: "q"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			model := NewModel(Config{AvailableTypes: []TypeChoice{{Style: StyleHunk, Available: true}}})
			model.Init()

			newModel, cmd := model.Update(tt.msg)
			m := newModel.(*Model)

			assert.NotNil(t, cmd, "should return quit command")
			assert.True(t, m.GetResult().Canceled, "result should be marked canceled")
			assert.Equal(t, StateCanceled, m.state)
		})
	}
}

func TestModel_Update_SpinnerTickMsg_DoesNotCancel(t *testing.T) {
	t.Parallel()
	model := NewModel(Config{AvailableTypes: []TypeChoice{{Style: StyleHunk, Available: true}}})
	model.Init()

	newModel, cmd := model.Update(spinner.TickMsg{})
	m := newModel.(*Model)

	assert.NotNil(t, cmd, "should return spinner tick command")
	assert.False(t, m.GetResult().Canceled, "spinner tick should not cancel the wizard")
	assert.Equal(t, StateSelectingType, m.state)
}
