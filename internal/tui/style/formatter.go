package style

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"

	"stackit.dev/stackit/internal/errors"
)

// GetLogShortColor returns a styled string with the color from StackitColors
func GetLogShortColor(text string, index int) string {
	if len(StackitColors) == 0 {
		return text
	}

	colorIndex := (index / 2) % len(StackitColors)
	color := StackitColors[colorIndex]

	// Convert RGB to hex color
	hexColor := lipgloss.Color(fmt.Sprintf("#%02x%02x%02x", color[0], color[1], color[2]))

	style := lipgloss.NewStyle().
		Foreground(hexColor)

	return style.Render(text)
}

// FormatShortLine applies color formatting to a short log line
func FormatShortLine(line string, circleIndex, arrowIndex int, isCurrent bool, overallIndent int) string {
	if circleIndex == -1 || arrowIndex == -1 {
		return line
	}

	// Find the arrow character and get its full width in bytes
	arrowRune := '▸'
	arrowWidth := utf8.RuneLen(arrowRune)

	// Split line into parts, skipping the full arrow character
	beforeArrow := line[:arrowIndex]
	afterArrow := line[arrowIndex+arrowWidth:]

	// Color the tree characters before the arrow
	var coloredBefore strings.Builder
	for i, char := range beforeArrow {
		coloredChar := GetLogShortColor(string(char), i)
		// Replace circle if current branch
		if char == '◯' && isCurrent {
			coloredChar = GetLogShortColor("◉", i)
		}
		coloredBefore.WriteString(coloredChar)
	}

	// Color the arrow character
	arrowChar := GetLogShortColor("▸", arrowIndex)

	// Color the branch name and details after the arrow
	coloredAfter := GetLogShortColor(afterArrow, circleIndex)

	// Calculate padding
	padding := overallIndent*2 + 3 - arrowIndex
	if padding > 0 {
		coloredBefore.WriteString(strings.Repeat(" ", padding))
	}

	return coloredBefore.String() + arrowChar + coloredAfter
}

// ColorBranchName colors a branch name based on whether it's current
func ColorBranchName(branchName string, isCurrent bool) string {
	name := branchName
	if isCurrent {
		name += " (current)"
	}
	return BranchStyle(isCurrent, false, false).Render(name)
}

// ColorBranchNameWithTrunk colors a branch name based on whether it's current and trunk status
func ColorBranchNameWithTrunk(branchName string, isCurrent bool, isTrunk bool) string {
	name := branchName
	if isCurrent {
		name += " (current)"
	}
	return BranchStyle(isCurrent, isTrunk, false).Render(name)
}

// ColorBranchNameBold colors a branch name with bold if current (green)
func ColorBranchNameBold(branchName string, isCurrent bool) string {
	return BranchStyle(isCurrent, false, false).Render(branchName)
}

// ColorBranchNameBoldWithTrunk colors a branch name with bold if current and trunk status
func ColorBranchNameBoldWithTrunk(branchName string, isCurrent bool, isTrunk bool) string {
	return BranchStyle(isCurrent, isTrunk, false).Render(branchName)
}

// BranchStyle returns the unified style for a branch name
func BranchStyle(isCurrent, isTrunk, isDim bool) lipgloss.Style {
	if isDim {
		return lipgloss.NewStyle().Foreground(colorDimValue())
	}
	if isCurrent {
		return lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Bold(true) // Bold Green
	}
	if isTrunk {
		// Distinct color for main/trunk: Pink (205)
		return lipgloss.NewStyle().Foreground(lipgloss.Color("205"))
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color("12")) // Bright Blue for others
}

// ColorNeedsRestack colors the "needs restack" text
func ColorNeedsRestack(text string) string {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("3")).
		Render(text)
}

// ColorDim makes text dim/gray (adaptive to terminal background)
func ColorDim(text string) string {
	return lipgloss.NewStyle().
		Foreground(colorDimValue()).
		Render(text)
}

// ColorMagenta colors text magenta
func ColorMagenta(text string) string {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("5")).
		Render(text)
}

// ColorPRNumber colors a PR number (yellow)
func ColorPRNumber(prNumber int) string {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("3")).
		Render(fmt.Sprintf("PR #%d", prNumber))
}

// ColorPRState colors PR state text based on state and draft status
func ColorPRState(state string, isDraft bool) string {
	if isDraft {
		return ColorDim("(Draft)")
	}

	switch state {
	case "APPROVED":
		return lipgloss.NewStyle().
			Foreground(lipgloss.Color("2")).
			Render("(Approved)")
	case "CHANGES_REQUESTED":
		return ColorMagenta("(Changes Requested)")
	case "REVIEW_REQUIRED":
		return ColorYellow("(Review Required)")
	default:
		// No review decision means review isn't required
		return ""
	}
}

// GetScopeColor returns a deterministic color for a scope string
func GetScopeColor(scope string) (lipgloss.Color, bool) {
	if scope == "" {
		return lipgloss.Color(""), false
	}
	// Simple hash to select from StackitColors
	var hash uint32
	for i := 0; i < len(scope); i++ {
		hash = uint32(scope[i]) + (hash << 6) + (hash << 16) - hash
	}
	colorIndex := int(hash) % len(StackitColors)
	color := StackitColors[colorIndex]
	return lipgloss.Color(fmt.Sprintf("#%02x%02x%02x", color[0], color[1], color[2])), true
}

// ColorScope colors a scope string deterministically
func ColorScope(scope string) string {
	if color, ok := GetScopeColor(scope); ok {
		return lipgloss.NewStyle().Foreground(color).Render("[" + scope + "]")
	}
	return ColorDim("[" + scope + "]")
}

// IconReviewApproved returns the approved icon (green checkmark)
func IconReviewApproved() string {
	return lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Render("✔")
}

// IconReviewChangesRequested returns the changes requested icon (orange warning)
func IconReviewChangesRequested() string {
	return lipgloss.NewStyle().Foreground(lipgloss.Color("208")).Render("⚠")
}

// IconCIPassing returns a green dot for passing CI
func IconCIPassing() string {
	return lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Render("●")
}

// IconCIFailing returns a red dot for failing CI
func IconCIFailing() string {
	return lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Render("●")
}

// IconCIPending returns a yellow dot for pending CI
func IconCIPending() string {
	return lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Render("●")
}

// IconFrozen returns the frozen icon (snowflake)
func IconFrozen() string {
	return lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Render("❄")
}

// IconLocked returns the locked icon (lock)
func IconLocked() string {
	return lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Render("🔒")
}

// ColorPRNumberByState colors PR number based on state
func ColorPRNumberByState(prNumber int, state string, isDraft bool) string {
	prefix := fmt.Sprintf("#%d", prNumber)
	if isDraft {
		return ColorDim(prefix)
	}
	switch state {
	case "MERGED":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("5")).Render(prefix) // purple
	case "CLOSED":
		return ColorDim(prefix)
	default:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Render(prefix) // cyan
	}
}

// FormatBranchModificationError formats a BranchModificationError with colors and helpful instructions
func FormatBranchModificationError(err *errors.BranchModificationError) string {
	var state, cmd string
	isLocked := err.IsLocked()
	switch {
	case isLocked && err.IsFrozen:
		state = fmt.Sprintf("locked (%s) and frozen", err.LockReason)
		cmd = "st unlock' and 'st unfreeze"
	case isLocked:
		state = fmt.Sprintf("locked (%s)", err.LockReason)
		cmd = "st unlock"
	case err.IsFrozen:
		state = "frozen"
		cmd = "st unfreeze"
	}

	branchNameColored := lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Render(err.BranchName)

	return fmt.Sprintf("branch %s is %s. Use '%s' to enable modifications",
		branchNameColored,
		state,
		cmd)
}
