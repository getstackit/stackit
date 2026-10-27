// Package style provides styling and coloring for the TUI.
package style

import (
	"image/color"
	"os"
	"sync"

	"charm.land/lipgloss/v2"
)

// Theme represents the terminal background type
type Theme int

const (
	// ThemeDark is for terminals with dark backgrounds
	ThemeDark Theme = iota
	// ThemeLight is for terminals with light backgrounds
	ThemeLight
)

// Color constants for consistent styling across TUI components.
// Use these instead of magic color numbers.
const (
	// ColorPrimary is magenta, used for emphasis, titles, and active states
	ColorPrimary = "205"
	// ColorSuccess is green, used for done/success states
	ColorSuccess = "42"
	// ColorSuccessAlt is an alternate green for selected items
	ColorSuccessAlt = "2"
	// ColorError is red, used for error states
	ColorError = "196"
	// ColorErrorAlt is an alternate red for inline errors
	ColorErrorAlt = "1"
	// ColorWarning is orange, used for warning states
	ColorWarning = "214"
	// ColorWarningAlt is yellow for inline warnings
	ColorWarningAlt = "3"
	// ColorSecondary is blue, used for branch names and secondary emphasis
	ColorSecondary = "12"
	// ColorAccent is cyan, used for selection backgrounds
	ColorAccent = "6"
	// ColorInsert is bright green, used for new/inserted items
	ColorInsert = "10"
	// ColorPending is dark gray, used for pending/inactive states
	ColorPending = "240"
	// ColorDim is medium gray, used for de-emphasized text
	ColorDimValue = "241"
	// ColorSubtle is medium-dark gray, used for secondary text
	ColorSubtleValue = "243"
	// ColorMerged is magenta variant, used for merged PR states
	ColorMerged = "5"
)

// DetectTheme returns whether the terminal has a dark or light background.
// The result is cached after the first call.
var DetectTheme = sync.OnceValue(func() Theme {
	if lipgloss.HasDarkBackground(os.Stdin, os.Stdout) {
		return ThemeDark
	}
	return ThemeLight
})

// IsDarkBackground returns true if the terminal has a dark background
func IsDarkBackground() bool {
	return DetectTheme() == ThemeDark
}

// Adaptive colors that work on both dark and light backgrounds
// Dark theme: use lighter grays, Light theme: use darker grays
func colorDimValue() color.Color {
	if IsDarkBackground() {
		return lipgloss.Color("245") // Light gray for dark backgrounds
	}
	return lipgloss.Color("240") // Darker gray for light backgrounds
}

func colorSubtleValue() color.Color {
	if IsDarkBackground() {
		return lipgloss.Color("246") // Medium gray for dark backgrounds (was 240, too faint)
	}
	return lipgloss.Color("243") // Medium-dark gray for light backgrounds
}

// DimColor returns the dim color value (adaptive to terminal background)
func DimColor() color.Color {
	return colorDimValue()
}

// DimStyle returns a style for dim/muted text (adaptive to terminal background)
func DimStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(colorDimValue())
}

// SubtleStyle returns a style for subtle/secondary text (adaptive to terminal background)
func SubtleStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(colorSubtleValue())
}

// StackitColors defines the color palette for branch visualization
// Matching the TypeScript version
var StackitColors = [][]int{
	{76, 203, 241},  // Light blue
	{77, 202, 125},  // Green
	{110, 173, 38},  // Dark green
	{245, 200, 0},   // Yellow
	{248, 144, 72},  // Orange
	{244, 98, 81},   // Red
	{235, 130, 188}, // Pink
	{159, 131, 228}, // Purple
	{80, 132, 243},  // Blue
}

// ColorRed colors text red
func ColorRed(text string) string {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorErrorAlt)).
		Render(text)
}

// ColorYellow colors text yellow
func ColorYellow(text string) string {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorWarningAlt)).
		Render(text)
}

// ColorCyan colors text cyan
func ColorCyan(text string) string {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorAccent)).
		Render(text)
}

// ColorGreen colors text green
func ColorGreen(text string) string {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorSuccessAlt)).
		Render(text)
}

// Selection cursor constants
const (
	SelectionCursor  = "▸ " // Cursor symbol for selected items
	SelectionPadding = "  " // Same width as cursor for unselected items
)

// Selection returns a style for selected items (created once, reused)
var Selection = sync.OnceValue(func() lipgloss.Style {
	return lipgloss.NewStyle().
		Background(lipgloss.Color(ColorAccent)).
		Foreground(lipgloss.Color("0")).
		Bold(true)
})

// SelectionCursorStyle returns the style for the selection cursor (created once, reused)
var SelectionCursorStyle = sync.OnceValue(func() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorAccent)).
		Bold(true)
})
