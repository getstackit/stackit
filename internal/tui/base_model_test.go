package tui

import (
	"testing"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
)

func TestBaseModel_SignalReady(t *testing.T) {
	t.Parallel()
	t.Run("closes channel on first call", func(t *testing.T) {
		t.Parallel()
		b := &BaseModel{}
		ch := make(chan struct{})
		b.SetReadyChan(ch)

		// Channel should be open
		select {
		case <-ch:
			t.Fatal("channel should not be closed yet")
		default:
		}

		b.SignalReady()

		// Channel should be closed now
		select {
		case <-ch:
			// OK
		default:
			t.Fatal("channel should be closed after SignalReady")
		}
	})

	t.Run("is idempotent - second call is no-op", func(t *testing.T) {
		t.Parallel()
		b := &BaseModel{}
		ch := make(chan struct{})
		b.SetReadyChan(ch)

		b.SignalReady()
		// Second call should not panic (closing already-closed channel would panic)
		b.SignalReady()
	})

	t.Run("handles nil channel gracefully", func(t *testing.T) {
		t.Parallel()
		b := &BaseModel{}
		// Should not panic
		b.SignalReady()
	})
}

func TestBaseModel_HandleCommonMsg_KeyMsg(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		msg         tea.Msg
		wantHandled bool
		wantQuit    bool
	}{
		{"ctrl+c quits", tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}, true, true},
		{"q quits", tea.KeyPressMsg{Code: 'q', Text: "q"}, true, true},
		{"other keys not handled", tea.KeyPressMsg{Code: 'a', Text: "a"}, false, false},
		{"enter not handled", tea.KeyPressMsg{Code: tea.KeyEnter}, false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			b := &BaseModel{}

			handled, cmd := b.HandleCommonMsg(tt.msg)

			if handled != tt.wantHandled {
				t.Errorf("handled = %v, want %v", handled, tt.wantHandled)
			}

			if tt.wantQuit {
				if cmd == nil {
					t.Error("expected quit command, got nil")
				}
			}
		})
	}
}

func TestBaseModel_HandleCommonMsg_WindowSizeMsg(t *testing.T) {
	t.Parallel()
	b := &BaseModel{}
	msg := tea.WindowSizeMsg{Width: 100, Height: 50}

	handled, cmd := b.HandleCommonMsg(msg)

	// Window size is handled but returns false so model can also process it
	if handled {
		t.Error("WindowSizeMsg should return handled=false")
	}
	if cmd != nil {
		t.Error("WindowSizeMsg should return nil cmd")
	}
	if b.Width != 100 {
		t.Errorf("Width = %d, want 100", b.Width)
	}
	if b.Height != 50 {
		t.Errorf("Height = %d, want 50", b.Height)
	}
}

func TestBaseModel_HandleCommonMsg_SpinnerTickMsg(t *testing.T) {
	t.Parallel()
	b := &BaseModel{}
	// Initialize spinner first
	b.InitSpinner()

	msg := spinner.TickMsg{}
	handled, cmd := b.HandleCommonMsg(msg)

	if !handled {
		t.Error("spinner.TickMsg should be handled")
	}
	if cmd == nil {
		t.Error("spinner.TickMsg should return a command for next tick")
	}
}

func TestBaseModel_HandleCommonMsg_UnknownMsg(t *testing.T) {
	t.Parallel()
	b := &BaseModel{}
	msg := struct{ custom string }{custom: "test"}

	handled, cmd := b.HandleCommonMsg(msg)

	if handled {
		t.Error("unknown message should not be handled")
	}
	if cmd != nil {
		t.Error("unknown message should return nil cmd")
	}
}

func TestBaseModel_DoneState(t *testing.T) {
	t.Parallel()
	b := &BaseModel{}

	if b.IsDone() {
		t.Error("should not be done initially")
	}

	b.MarkDone()

	if !b.IsDone() {
		t.Error("should be done after MarkDone")
	}

	// Verify Done field directly
	if !b.Done {
		t.Error("Done field should be true")
	}
}

func TestBaseModel_InitSpinner(t *testing.T) {
	t.Parallel()
	b := &BaseModel{}
	cmd := b.InitSpinner()

	if cmd == nil {
		t.Error("InitSpinner should return a tick command")
	}

	// Verify spinner View() returns something (indicates spinner is initialized)
	view := b.Spinner.View()
	if view == "" {
		t.Error("Spinner should be initialized and render a view")
	}
}

func TestBaseModel_SetReadyChan(t *testing.T) {
	t.Parallel()
	b := &BaseModel{}
	ch := make(chan struct{})

	b.SetReadyChan(ch)

	// The channel should be stored (verified indirectly via SignalReady)
	b.SignalReady()

	select {
	case <-ch:
		// OK - channel was closed
	default:
		t.Error("SetReadyChan did not store the channel correctly")
	}
}
