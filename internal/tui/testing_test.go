package tui

import (
	"sync"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
)

func TestMockRunner(t *testing.T) {
	t.Parallel()
	t.Run("tracks started and stopped state", func(t *testing.T) {
		t.Parallel()
		runner := NewMockRunner()
		assert.False(t, runner.IsHealthy())

		runner.Start()
		assert.True(t, runner.IsHealthy())

		runner.Cleanup()
		assert.False(t, runner.IsHealthy())
	})

	t.Run("records messages", func(t *testing.T) {
		t.Parallel()
		runner := NewMockRunner()
		runner.Start()

		type testMsg struct{ value int }
		runner.Send(testMsg{value: 1})
		runner.Send(testMsg{value: 2})

		messages := runner.Messages()
		assert.Len(t, messages, 2)
		assert.Equal(t, testMsg{value: 1}, messages[0])
		assert.Equal(t, testMsg{value: 2}, messages[1])
	})

	t.Run("reset clears state", func(t *testing.T) {
		t.Parallel()
		runner := NewMockRunner()
		runner.Start()
		runner.Send(tea.KeyPressMsg{Code: tea.KeyEnter})

		runner.Reset()
		assert.False(t, runner.started)
		assert.False(t, runner.stopped)
		assert.Equal(t, 0, runner.MessageCount())
	})

	t.Run("is thread-safe", func(t *testing.T) {
		t.Parallel()
		runner := NewMockRunner()
		runner.Start()

		var wg sync.WaitGroup
		for i := range 100 {
			wg.Add(1)
			go func(n int) {
				defer wg.Done()
				runner.Send(tea.KeyPressMsg{Code: rune('a' + n%26), Text: string(rune('a' + n%26))})
			}(i)
		}
		wg.Wait()

		assert.Equal(t, 100, runner.MessageCount())
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

		assert.Equal(t, 2, recorder.Count())
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
		assert.True(t, found)

		notFound := recorder.HasMessage(func(msg tea.Msg) bool {
			if m, ok := msg.(testMsg); ok {
				return m.id == "nonexistent"
			}
			return false
		})
		assert.False(t, notFound)
	})

	t.Run("reset clears messages", func(t *testing.T) {
		t.Parallel()
		recorder := NewMessageRecorder()
		recorder.Record(tea.KeyPressMsg{Code: tea.KeyEnter})

		recorder.Reset()
		assert.Equal(t, 0, recorder.Count())
	})

	t.Run("is thread-safe", func(t *testing.T) {
		t.Parallel()
		recorder := NewMessageRecorder()

		var wg sync.WaitGroup
		for i := range 100 {
			wg.Add(1)
			go func(n int) {
				defer wg.Done()
				recorder.Record(tea.KeyPressMsg{Code: rune('a' + n%26), Text: string(rune('a' + n%26))})
			}(i)
		}
		wg.Wait()

		assert.Equal(t, 100, recorder.Count())
	})
}
