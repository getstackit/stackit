package dashboard

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

// View keeps the companion usable while its local state is loading. The full
// narrow layout is intentionally kept separate from the reload machinery.
func (m *companionModel) View() tea.View {
	content := "Loading local stack state…"
	if !m.loading && m.errorMessage != "" {
		content = "Unable to reload local stack state: " + m.errorMessage
	}
	if m.Width > 0 {
		content = truncateCompanionLine(content, m.Width)
	}
	v := tea.NewView(strings.TrimSpace(content))
	v.AltScreen = true
	return v
}

func truncateCompanionLine(value string, width int) string {
	if width <= 0 || len(value) <= width {
		return value
	}
	if width <= len("…") {
		return "…"
	}
	return value[:width-len("…")] + "…"
}
