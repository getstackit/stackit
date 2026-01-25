package split

import (
	"stackit.dev/stackit/internal/tui/style"
)

// Styles contains styling for the split component using shared style types
type Styles struct {
	Header    style.HeaderStyles
	Selection style.SelectionStyles
	Status    style.StatusStyles
	Common    style.CommonStyles
	Layout    style.LayoutStyles
	Icons     style.StatusIcons
}

// DefaultStyles returns the default styles for the split component
func DefaultStyles() Styles {
	return Styles{
		Header:    style.DefaultHeaderStyles(),
		Selection: style.DefaultSelectionStyles(),
		Status:    style.DefaultStatusStyles(),
		Common:    style.DefaultCommonStyles(),
		Layout:    style.DefaultLayoutStyles(),
		Icons:     style.DefaultStatusIcons(),
	}
}
