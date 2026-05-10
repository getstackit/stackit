// Package foreach provides a TUI component for displaying the progress of foreach command execution.
package foreach

import (
	"charm.land/lipgloss/v2"

	"github.com/getstackit/stackit/internal/tui/core"
	"github.com/getstackit/stackit/internal/tui/style"
)

// Item represents a branch being processed
type Item struct {
	BranchName string
	Status     core.Status
	Output     string
	Error      error
}

// Styles defines the visual styling for the foreach component.
// It uses the shared style definitions from internal/tui/style for consistency.
type Styles struct {
	SpinnerStyle lipgloss.Style
	DoneStyle    lipgloss.Style
	ErrorStyle   lipgloss.Style
	BranchStyle  lipgloss.Style
	OutputStyle  lipgloss.Style
	DimStyle     lipgloss.Style
}

// DefaultStyles returns the default styles for the foreach component,
// using shared styles from the style package for consistency.
func DefaultStyles() Styles {
	statusStyles := style.DefaultStatusStyles()
	commonStyles := style.DefaultCommonStyles()
	return Styles{
		SpinnerStyle: commonStyles.Spinner,
		DoneStyle:    statusStyles.Done,
		ErrorStyle:   statusStyles.Error,
		BranchStyle:  commonStyles.Branch.Bold(true),
		OutputStyle:  commonStyles.Dim,
		DimStyle:     commonStyles.Subtle,
	}
}
