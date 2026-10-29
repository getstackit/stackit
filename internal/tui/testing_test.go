package tui

import (
	"sync"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/require"
)

func TestMockRunner(t *testing.T) {
	t.Parallel()
	t.Run("tracks started and stopped state", func(t *testing.T) {
		t.Parallel()
		runner := NewMockRunner()
		require.False(t, runner.IsHealthy())

		runner.Start()
		require.True(t, runner.IsHealthy())

		runner.Cleanup()
		require.False(t, runner.IsHealthy())
	})

	t.Run("records messages", func(t *testing.T) {
		t.Parallel()
		runner := NewMockRunner()
		runner.Start()

		type testMsg struct{ value int }
		runner.Send(testMsg{value: 1})
		runner.Send(testMsg{value: 2})

		messages := runner.Messages()
		require.Len(t, messages, 2)
		require.Equal(t, testMsg{value: 1}, messages[0])
		require.Equal(t, testMsg{value: 2}, messages[1])
	})

	t.Run("reset clears state", func(t *testing.T) {
		t.Parallel()
		runner := NewMockRunner()
		runner.Start()
		runner.Send(tea.KeyPressMsg{Code: tea.KeyEnter})

		runner.Reset()
		require.False(t, runner.started)
		require.False(t, runner.stopped)
		require.Equal(t, 0, runner.MessageCount())
	})

	t.Run("is thread-safe", func(t *testing.T) {
		t.Parallel()
		runner := NewMockRunner()
		runner.Start()

		var wg sync.WaitGroup
		for i := range 100 {
			wg.Go(func() {
				runner.Send(tea.KeyPressMsg{Code: rune('a' + i%26), Text: string(rune('a' + i%26))})
			})
		}
		wg.Wait()

		require.Equal(t, 100, runner.MessageCount())
	})
}

func TestMessageRecorder(t *testing.T) {
	t.Parallel()
	t.Run("records messages", func(t *testing.T) {
		t.Parallel()
		recorder := NewMessageRecorder()

		type testMsg struct{ id string }
		recorder.Record(testMsg{id: "first"})
		recorder.Record(testMsg{id: "second"})

		require.Equal(t, 2, recorder.Count())
	})

	t.Run("HasMessage finds matching messages", func(t *testing.T) {
		t.Parallel()
		recorder := NewMessageRecorder()

		type testMsg struct{ id string }
		recorder.Record(testMsg{id: "target"})
		recorder.Record(testMsg{id: "other"})

		found := recorder.HasMessage(func(msg tea.Msg) bool {
			if m, ok := msg.(testMsg); ok {
				return m.id == "target"
			}
			return false
		})
		require.True(t, found)

		notFound := recorder.HasMessage(func(msg tea.Msg) bool {
			if m, ok := msg.(testMsg); ok {
				return m.id == "nonexistent"
			}
			return false
		})
		require.False(t, notFound)
	})

	t.Run("reset clears messages", func(t *testing.T) {
		t.Parallel()
		recorder := NewMessageRecorder()
		recorder.Record(tea.KeyPressMsg{Code: tea.KeyEnter})

		recorder.Reset()
		require.Equal(t, 0, recorder.Count())
	})

	t.Run("is thread-safe", func(t *testing.T) {
		t.Parallel()
		recorder := NewMessageRecorder()

		var wg sync.WaitGroup
		for i := range 100 {
			wg.Go(func() {
				recorder.Record(tea.KeyPressMsg{Code: rune('a' + i%26), Text: string(rune('a' + i%26))})
			})
		}
		wg.Wait()

		require.Equal(t, 100, recorder.Count())
	})
}
