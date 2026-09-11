package style

import "charm.land/lipgloss/v2"

// Status markers lead individual item rows in streaming and TUI output (the
// lines printed beneath a phase header). Each glyph is a single display cell, so
// successive rows align in one column regardless of which status leads them.
//
// This is a deliberate two-tier visual language: emoji (📥 🧹 📚 ✅ ✨) are
// reserved for section headers and summaries, while these thin, colored glyphs
// mark the items underneath. Keeping the item markers single-width is what makes
// the text after them line up.
const (
	// GlyphSuccess marks a completed item.
	GlyphSuccess = "✓"
	// GlyphWarning marks a skipped item or an item that needs attention.
	GlyphWarning = "⚠"
	// GlyphProgress marks an item whose work has started but not yet finished.
	GlyphProgress = "→"
)

// MarkSuccess returns the green ✓ shown on completed item rows.
func MarkSuccess() string {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(ColorSuccess)).Render(GlyphSuccess)
}

// MarkWarning returns the orange ⚠ shown on skipped/attention item rows.
func MarkWarning() string {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(ColorWarning)).Render(GlyphWarning)
}

// MarkProgress returns the dimmed → shown on in-progress item rows.
func MarkProgress() string {
	return DimStyle().Render(GlyphProgress)
}
