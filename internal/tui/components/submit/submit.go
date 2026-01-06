// Package submit provides a TUI component for displaying the progress of a stack submission.
package submit

import (
	"github.com/charmbracelet/lipgloss"

	"stackit.dev/stackit/internal/tui/style"
)

// Item represents a branch being submitted
type Item struct {
	BranchName string
	Action     string // "create" or "update"
	PRNumber   *int
	Status     string // "pending", "submitting", "done", "error"
	IsSkipped  bool
	SkipReason string
	URL        string
	Error      error
}

// Styles defines the visual styling for the submit component
type Styles struct {
	SpinnerStyle lipgloss.Style
	DoneStyle    lipgloss.Style
	ErrorStyle   lipgloss.Style
	BranchStyle  lipgloss.Style
	URLStyle     lipgloss.Style
	DimStyle     lipgloss.Style
}

// DefaultStyles returns the default styles for the submit component
func DefaultStyles() Styles {
	return Styles{
		SpinnerStyle: lipgloss.NewStyle().Foreground(lipgloss.Color("205")),
		DoneStyle:    lipgloss.NewStyle().Foreground(lipgloss.Color("42")),
		ErrorStyle:   lipgloss.NewStyle().Foreground(lipgloss.Color("196")),
		BranchStyle:  style.BranchStyle(false, false, false).Bold(true),
		URLStyle:     lipgloss.NewStyle().Foreground(lipgloss.Color("245")),
		DimStyle:     lipgloss.NewStyle().Foreground(lipgloss.Color("240")),
	}
}

const (
	// StatusSubmitting indicates the branch is currently being submitted
	StatusSubmitting = "submitting"
	// StatusSyncing indicates the branch metadata is being synced
	StatusSyncing = "syncing"
	// StatusDone indicates the branch submission was successful
	StatusDone = "done"
	// StatusError indicates the branch submission failed
	StatusError = "error"
	// StatusPending indicates the branch is waiting to be submitted
	StatusPending = "pending"
	// SkipReasonNoChanges indicates the branch was skipped because it has no changes
	SkipReasonNoChanges = "no changes"
)
