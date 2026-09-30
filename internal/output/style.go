package output

import "github.com/getstackit/stackit/internal/tui/style"

// Styling helpers for composing user-facing messages. These live in the output
// (presentation) layer so business-logic actions can render colored fragments
// without importing internal/tui. They delegate to internal/tui/style, which
// remains the single source of the actual lipgloss rendering.

// BranchName renders a non-current branch name, colored.
func BranchName(name string) string {
	return style.ColorBranchName(name)
}

// CurrentBranch renders the current branch name with a "(current)" marker.
func CurrentBranch(name string) string {
	return style.ColorCurrentBranch(name)
}

// Branch renders a branch name, colored, with a "(current)" marker when isCurrent.
// Prefer BranchName/CurrentBranch when the condition is known at the call site.
func Branch(name string, isCurrent bool) string {
	return style.ColorBranchNameIf(name, isCurrent)
}

// Dim renders text in a dim/gray style.
func Dim(text string) string { return style.ColorDim(text) }

// Cyan renders text in cyan.
func Cyan(text string) string { return style.ColorCyan(text) }

// Red renders text in red.
func Red(text string) string { return style.ColorRed(text) }

// Yellow renders text in yellow.
func Yellow(text string) string { return style.ColorYellow(text) }

// Green renders text in green.
func Green(text string) string { return style.ColorGreen(text) }
