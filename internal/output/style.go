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

// BranchNameWithTrunk renders a non-current branch name, with trunk in a distinct color.
func BranchNameWithTrunk(name string, isTrunk bool) string {
	return style.ColorBranchNameWithTrunk(name, false, isTrunk)
}

// BranchWithTrunk is like Branch but renders the trunk branch in a distinct color.
func BranchWithTrunk(name string, isCurrent, isTrunk bool) string {
	return style.ColorBranchNameWithTrunk(name, isCurrent, isTrunk)
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

// Magenta renders text in magenta.
func Magenta(text string) string { return style.ColorMagenta(text) }

// Scope renders a scope label.
func Scope(scope string) string { return style.ColorScope(scope) }

// NeedsRestack renders a "needs restack" hint.
func NeedsRestack(text string) string { return style.ColorNeedsRestack(text) }

// PRNumber renders a pull-request number.
func PRNumber(prNumber int) string { return style.ColorPRNumber(prNumber) }

// RenderMarkdown renders markdown content for the terminal.
func RenderMarkdown(content string) string { return style.RenderMarkdown(content) }

// IconLocked returns the lock status icon.
func IconLocked() string { return style.IconLocked() }

// IconFrozen returns the freeze status icon.
func IconFrozen() string { return style.IconFrozen() }
