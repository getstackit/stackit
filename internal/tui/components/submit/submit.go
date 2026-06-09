// Package submit provides a TUI component for displaying the progress of a stack submission.
package submit

import (
	"charm.land/lipgloss/v2"

	"github.com/getstackit/stackit/internal/tui/style"
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

// Styles defines the visual styling for the submit component.
// It uses the shared style definitions from internal/tui/style for consistency.
type Styles struct {
	SpinnerStyle lipgloss.Style
	DoneStyle    lipgloss.Style
	ErrorStyle   lipgloss.Style
	BranchStyle  lipgloss.Style
	DimStyle     lipgloss.Style
}

// DefaultStyles returns the default styles for the submit component,
// using shared styles from the style package for consistency.
func DefaultStyles() Styles {
	statusStyles := style.DefaultStatusStyles()
	commonStyles := style.DefaultCommonStyles()
	return Styles{
		SpinnerStyle: commonStyles.Spinner,
		DoneStyle:    statusStyles.Done,
		ErrorStyle:   statusStyles.Error,
		BranchStyle:  commonStyles.Branch.Bold(true),
		DimStyle:     commonStyles.Subtle,
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
	// ActionUpdate indicates the branch will update an existing PR
	ActionUpdate = "update"
	// ActionCreate indicates the branch will create a new PR
	ActionCreate = "create"
)
