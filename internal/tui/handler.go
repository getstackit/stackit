// Package tui provides terminal UI utilities.
package tui

import tea "charm.land/bubbletea/v2"

// Sender is the interface for sending messages to a TUI.
// Both *Runner and *MockRunner implement this interface.
type Sender interface {
	// Send sends a message to the TUI model.
	Send(msg tea.Msg)

	// Pause pauses TUI rendering for interactive prompts.
	Pause()

	// Resume resumes TUI rendering after a pause.
	Resume()

	// Wait blocks until the TUI exits.
	Wait()

	// Cleanup performs terminal cleanup.
	Cleanup()

	// IsHealthy returns true if the TUI is running and responsive.
	IsHealthy() bool
}
