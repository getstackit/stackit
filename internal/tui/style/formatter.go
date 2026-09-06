package style

import (
	"github.com/getstackit/stackit/internal/git"

	"fmt"
	"image/color"
	"strings"
	"unicode/utf8"

	"charm.land/lipgloss/v2"

	"github.com/getstackit/stackit/internal/errors"
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

// ColorBranchName colors a branch name that is not the current branch.
func ColorBranchName(branchName string) string {
	return BranchStyle(BranchStyleOpts{}).Render(branchName)
}

// ColorCurrentBranch colors the checked-out branch and appends its
// " (current)" marker.
func ColorCurrentBranch(branchName string) string {
	return BranchStyle(BranchStyleOpts{IsCurrent: true}).Render(branchName + " (current)")
}

// ColorBranchNameIf renders the current-branch style (with the " (current)"
// marker) when isCurrent, and the plain style otherwise. For call sites where
// currency is only known at runtime; prefer ColorBranchName/ColorCurrentBranch
// when it is fixed.
func ColorBranchNameIf(branchName string, isCurrent bool) string {
	if isCurrent {
		return ColorCurrentBranch(branchName)
	}
	return ColorBranchName(branchName)
}

// ColorBranchNameWithTrunk colors a branch name based on whether it's current and trunk status
func ColorBranchNameWithTrunk(branchName string, opts BranchStyleOpts) string {
	name := branchName
	if opts.IsCurrent {
		name += " (current)"
	}
	return BranchStyle(opts).Render(name)
}

// ColorBranchNamePlain colors a branch name by current/trunk status without
// appending the " (current)" marker, for callers that already convey
// currency another way (e.g. a leading cursor or row highlight).
func ColorBranchNamePlain(branchName string, opts BranchStyleOpts) string {
	return BranchStyle(opts).Render(branchName)
}

// BranchStyleOpts configures the appearance BranchStyle renders.
type BranchStyleOpts struct {
	IsCurrent bool
	IsTrunk   bool
	IsDim     bool
}

// BranchStyle returns the unified style for a branch name
func BranchStyle(opts BranchStyleOpts) lipgloss.Style {
	if opts.IsDim {
		return lipgloss.NewStyle().Foreground(colorDimValue())
	}
	if opts.IsCurrent {
		return lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Bold(true) // Bold Green
	}
	if opts.IsTrunk {
		// Distinct color for main/trunk: Pink (205)
		return lipgloss.NewStyle().Foreground(lipgloss.Color("205"))
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color("12")) // Bright Blue for others
}

// ColorNeedsRestack colors restack suggestion text.
func ColorNeedsRestack(text string) string {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("3")).
		Render(text)
}

// Bold renders text bold with no color of its own. Used for section headers,
// where bold alone is enough hierarchy against indented rows.
func Bold(text string) string {
	return lipgloss.NewStyle().Bold(true).Render(text)
}

// ColorDim makes text dim/gray (adaptive to terminal background)
func ColorDim(text string) string {
	return lipgloss.NewStyle().
		Foreground(colorDimValue()).
		Render(text)
}

// ColorSHA colors a git SHA (yellow)
func ColorSHA(sha string) string {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("3")).
		Render(sha)
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

// GetScopeColor returns a deterministic color for a scope string
func GetScopeColor(scope string) (color.Color, bool) {
	if scope == "" {
		return lipgloss.NoColor{}, false
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
func ColorPRNumberByState(prNumber int, state git.PRState, isDraft bool) string {
	prefix := fmt.Sprintf("#%d", prNumber)
	if isDraft {
		return ColorDim(prefix)
	}
	switch state {
	case git.PRStateMerged:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("5")).Render(prefix) // purple
	case git.PRStateClosed:
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
