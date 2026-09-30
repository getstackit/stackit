// Package tui provides terminal UI utilities.
package tui

// ProgressMsg updates progress state.
// Use this to communicate progress updates to TUI models.
type ProgressMsg struct {
	Completed int // Number of completed operations
	Total     int // Total number of operations
}

// PhaseMsg indicates a phase state change.
// Use this to communicate phase transitions to TUI models.
type PhaseMsg struct {
	Phase  string // Name of the phase
	Status Status // Current status of the phase
	Detail string // Optional detail message
}

// CompleteMsg signals operation completion.
// Use this to indicate that the overall operation is done.
type CompleteMsg struct {
	Success bool   // True if operation succeeded
	Summary string // Summary message to display
	Error   error  // Error if Success is false
}
