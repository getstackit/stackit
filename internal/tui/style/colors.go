// Package style provides styling and coloring for the TUI.
package style

import (
	"sync"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// Theme represents the terminal background type
type Theme int

const (
	// ThemeDark is for terminals with dark backgrounds
	ThemeDark Theme = iota
	// ThemeLight is for terminals with light backgrounds
	ThemeLight
)

var (
	detectedTheme Theme
	themeOnce     sync.Once
)

// DetectTheme returns whether the terminal has a dark or light background.
// The result is cached after the first call.
func DetectTheme() Theme {
	themeOnce.Do(func() {
		if termenv.HasDarkBackground() {
			detectedTheme = ThemeDark
		} else {
			detectedTheme = ThemeLight
		}
	})
	return detectedTheme
}

// IsDarkBackground returns true if the terminal has a dark background
func IsDarkBackground() bool {
	return DetectTheme() == ThemeDark
}

// Adaptive colors that work on both dark and light backgrounds
// Dark theme: use lighter grays, Light theme: use darker grays
func colorDimValue() lipgloss.Color {
	if IsDarkBackground() {
		return lipgloss.Color("245") // Light gray for dark backgrounds
	}
	return lipgloss.Color("240") // Darker gray for light backgrounds
}

func colorSubtleValue() lipgloss.Color {
	if IsDarkBackground() {
		return lipgloss.Color("240") // Medium gray for dark backgrounds
	}
	return lipgloss.Color("245") // Medium-dark gray for light backgrounds
}

// DimColor returns the dim color value (adaptive to terminal background)
func DimColor() lipgloss.Color {
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
		Foreground(lipgloss.Color("1")).
		Render(text)
}

// ColorYellow colors text yellow
func ColorYellow(text string) string {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("3")).
		Render(text)
}

// ColorCyan colors text cyan
func ColorCyan(text string) string {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("6")).
		Render(text)
}

// ColorGreen colors text green
func ColorGreen(text string) string {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("2")).
		Render(text)
}

// Selection cursor constants
const (
	SelectionCursor  = "▸ " // Cursor symbol for selected items
	SelectionPadding = "  " // Same width as cursor for unselected items
)

// Selection returns a style for selected items
func Selection() lipgloss.Style {
	return lipgloss.NewStyle().
		Background(lipgloss.Color("6")). // Cyan background
		Foreground(lipgloss.Color("0")). // Black foreground
		Bold(true)
}

// SelectionCursorStyle returns the style for the selection cursor
func SelectionCursorStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("6")). // Cyan
		Bold(true)
}
