package style

import "charm.land/lipgloss/v2"

// StatusStyles contains styles for status indicators used across TUI components.
// This provides a consistent look for pending, active, done, and error states.
type StatusStyles struct {
	// Pending style for items not yet started
	Pending lipgloss.Style
	// Active style for items currently in progress
	Active lipgloss.Style
	// Done style for successfully completed items
	Done lipgloss.Style
	// Error style for failed items
	Error lipgloss.Style
	// Warning style for items needing attention
	Warning lipgloss.Style
}

// StatusIcons contains icon characters for status indicators.
type StatusIcons struct {
	Pending string
	Active  string
	Done    string
	Error   string
	Warning string
}

// DefaultStatusStyles returns the standard status styles for stackit TUIs.
func DefaultStatusStyles() StatusStyles {
	return StatusStyles{
		Pending: lipgloss.NewStyle().Foreground(lipgloss.Color(ColorPending)),
		Active:  lipgloss.NewStyle().Foreground(lipgloss.Color(ColorPrimary)),
		Done:    lipgloss.NewStyle().Foreground(lipgloss.Color(ColorSuccess)),
		Error:   lipgloss.NewStyle().Foreground(lipgloss.Color(ColorError)),
		Warning: lipgloss.NewStyle().Foreground(lipgloss.Color(ColorWarning)),
	}
}

// DefaultStatusIcons returns the standard status icons for stackit TUIs.
func DefaultStatusIcons() StatusIcons {
	return StatusIcons{
		Pending: "○",
		Active:  "◐",
		Done:    "✓",
		Error:   "✗",
		Warning: "⚠",
	}
}

// CommonStyles contains commonly used text styles.
type CommonStyles struct {
	// Bold style for emphasized text
	Bold lipgloss.Style
	// Dim style for de-emphasized text (adaptive to terminal background)
	Dim lipgloss.Style
	// Subtle style for secondary text (adaptive to terminal background)
	Subtle lipgloss.Style
	// Branch style for branch names
	Branch lipgloss.Style
	// URL style for links
	URL lipgloss.Style
	// Spinner style for animated spinners
	Spinner lipgloss.Style
}

// DefaultCommonStyles returns the standard common styles for stackit TUIs.
func DefaultCommonStyles() CommonStyles {
	return CommonStyles{
		Bold:    lipgloss.NewStyle().Bold(true),
		Dim:     DimStyle(),
		Subtle:  SubtleStyle(),
		Branch:  BranchStyle(BranchStyleOpts{}),
		URL:     DimStyle(),
		Spinner: lipgloss.NewStyle().Foreground(lipgloss.Color(ColorPrimary)),
	}
}

// HeaderStyles contains styles for titles and headers.
type HeaderStyles struct {
	// Title is bold magenta for primary headers
	Title lipgloss.Style
	// Subtitle is for secondary headers with top margin
	Subtitle lipgloss.Style
	// Help is gray for help text
	Help lipgloss.Style
}

// DefaultHeaderStyles returns the standard header styles for stackit TUIs.
func DefaultHeaderStyles() HeaderStyles {
	return HeaderStyles{
		Title:    lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ColorPrimary)),
		Subtitle: lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ColorPrimary)).MarginTop(1),
		Help:     lipgloss.NewStyle().Foreground(lipgloss.Color(ColorDimValue)),
	}
}

// LayoutStyles contains styles for layout and spacing.
type LayoutStyles struct {
	// Container is the standard container with margins
	Container lipgloss.Style
	// Compact is minimal spacing
	Compact lipgloss.Style
	// Spacious is extra spacing
	Spacious lipgloss.Style
}

// Standard margin values for consistent layout.
const (
	MarginVertical   = 1
	MarginHorizontal = 2
)

// DefaultLayoutStyles returns the standard layout styles for stackit TUIs.
func DefaultLayoutStyles() LayoutStyles {
	return LayoutStyles{
		Container: lipgloss.NewStyle().Margin(MarginVertical, MarginHorizontal),
		Compact:   lipgloss.NewStyle().Margin(0, 1),
		Spacious:  lipgloss.NewStyle().Margin(2, 3),
	}
}

// SelectionStyles contains styles for selection UI elements.
type SelectionStyles struct {
	// Cursor is bold magenta for the selection indicator
	Cursor lipgloss.Style
	// Selected is green for currently selected items
	Selected lipgloss.Style
	// Unselected is gray for non-selected items
	Unselected lipgloss.Style
	// Highlighted is for items with cursor focus (different from selected)
	Highlighted lipgloss.Style
}

// DefaultSelectionStyles returns the standard selection styles for stackit TUIs.
func DefaultSelectionStyles() SelectionStyles {
	return SelectionStyles{
		Cursor:      lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ColorPrimary)),
		Selected:    lipgloss.NewStyle().Foreground(lipgloss.Color(ColorSuccessAlt)),
		Unselected:  lipgloss.NewStyle().Foreground(lipgloss.Color(ColorSubtleValue)),
		Highlighted: lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ColorPrimary)),
	}
}

// InsertStyle returns the style for insert/new item indicators.
func InsertStyle() lipgloss.Style {
	return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ColorInsert))
}

// HelpKeyStyle returns a style for help key bindings.
// Uses a more visible color than the default bubbles help style.
func HelpKeyStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(colorSubtleValue())
}

// HelpDescStyle returns a style for help descriptions.
// Uses a more visible color than the default bubbles help style.
func HelpDescStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(colorDimValue())
}

// HelpSeparatorStyle returns a style for help separators (the • between items).
func HelpSeparatorStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(colorDimValue())
}
