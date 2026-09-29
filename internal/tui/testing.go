// Package tui provides terminal UI utilities.
package tui

import (
	"slices"
	"sync"

	tea "charm.land/bubbletea/v2"
)

// MockRunner is a test double for Runner.
// It records all messages sent to it and provides methods to inspect them.
type MockRunner struct {
	mu       sync.Mutex
	messages []tea.Msg
	started  bool
	stopped  bool
}

// NewMockRunner creates a new MockRunner.
func NewMockRunner() *MockRunner {
	return &MockRunner{messages: make([]tea.Msg, 0)}
}

// Start marks the runner as started.
func (m *MockRunner) Start() { m.started = true }

// Cleanup marks the runner as stopped.
func (m *MockRunner) Cleanup() { m.stopped = true }

// Pause is a no-op for the mock runner.
func (m *MockRunner) Pause() {}

// Resume is a no-op for the mock runner.
func (m *MockRunner) Resume() {}

// Wait is a no-op for the mock runner.
func (m *MockRunner) Wait() {}

// IsHealthy returns true if the runner has been started and not stopped.
func (m *MockRunner) IsHealthy() bool { return m.started && !m.stopped }

// Send records a message for later inspection.
func (m *MockRunner) Send(msg tea.Msg) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages = append(m.messages, msg)
}

// Messages returns a copy of all recorded messages.
func (m *MockRunner) Messages() []tea.Msg {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]tea.Msg{}, m.messages...)
}

// Reset clears all recorded messages and state.
func (m *MockRunner) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages = make([]tea.Msg, 0)
	m.started = false
	m.stopped = false
}

// MessageCount returns the number of recorded messages.
func (m *MockRunner) MessageCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.messages)
}

// MessageRecorder records messages for test assertions.
// It can be used to verify that specific messages were sent during tests.
type MessageRecorder struct {
	mu       sync.Mutex
	messages []tea.Msg
}

// NewMessageRecorder creates a new MessageRecorder.
func NewMessageRecorder() *MessageRecorder {
	return &MessageRecorder{messages: make([]tea.Msg, 0)}
}

// Record adds a message to the recorder.
func (r *MessageRecorder) Record(msg tea.Msg) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.messages = append(r.messages, msg)
}

// HasMessage returns true if any recorded message matches the predicate.
func (r *MessageRecorder) HasMessage(predicate func(tea.Msg) bool) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.ContainsFunc(r.messages, predicate)
}

// Reset clears all recorded messages.
func (r *MessageRecorder) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.messages = make([]tea.Msg, 0)
}

// Count returns the number of recorded messages.
func (r *MessageRecorder) Count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.messages)
}
