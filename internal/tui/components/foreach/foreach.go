// Package foreach provides a TUI component for displaying the progress of foreach command execution.
package foreach

import (
	"github.com/charmbracelet/lipgloss"

	"stackit.dev/stackit/internal/tui/style"
)

// Item represents a branch being processed
type Item struct {
	BranchName string
	Status     string // "pending", "running", "done", "error"
	Output     string
	Error      error
}

// Styles defines the visual styling for the foreach component
type Styles struct {
	SpinnerStyle lipgloss.Style
	DoneStyle    lipgloss.Style
	ErrorStyle   lipgloss.Style
	BranchStyle  lipgloss.Style
	OutputStyle  lipgloss.Style
	DimStyle     lipgloss.Style
}

// DefaultStyles returns the default styles for the foreach component
func DefaultStyles() Styles {
	return Styles{
		SpinnerStyle: lipgloss.NewStyle().Foreground(lipgloss.Color("205")),
		DoneStyle:    lipgloss.NewStyle().Foreground(lipgloss.Color("42")),
		ErrorStyle:   lipgloss.NewStyle().Foreground(lipgloss.Color("196")),
		BranchStyle:  style.BranchStyle(false, false, false).Bold(true),
		OutputStyle:  style.DimStyle(),
		DimStyle:     style.SubtleStyle(),
	}
}

const (
	// StatusRunning indicates the branch is currently executing
	StatusRunning = "running"
	// StatusDone indicates the branch execution was successful
	StatusDone = "done"
	// StatusError indicates the branch execution failed
	StatusError = "error"
	// StatusPending indicates the branch is waiting to be executed
	StatusPending = "pending"
)
