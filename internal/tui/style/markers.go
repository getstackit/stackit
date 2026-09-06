package style

import (
	"strings"

	"charm.land/lipgloss/v2"
)

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
	// GlyphNote marks a consequence worth pointing out — a change the reader
	// could not have predicted, such as a branch being reparented.
	GlyphNote = "↳"
	// GlyphStepDone marks a finished or active step in a pipeline indicator.
	GlyphStepDone = "●"
	// GlyphStepPending marks a step a pipeline has not reached yet.
	GlyphStepPending = "○"
)

// StepPips renders a fixed-length pipeline position as pips — e.g. "●●●●○" for
// step 4 of 5. Steps before the current one take the completed color, the
// current one takes the spinner's color so it reads as part of the active line,
// and steps not yet reached stay dim.
//
// This is for a pipeline whose stages are known up front, where the useful
// signal is "how far along the sequence are we". It deliberately says nothing
// about how much work each stage holds: a stage is one pip whether it touches
// one branch or twenty.
func StepPips(current, total int) string {
	if total <= 0 || current <= 0 {
		return ""
	}

	done := lipgloss.NewStyle().Foreground(lipgloss.Color(ColorSuccess))
	active := lipgloss.NewStyle().Foreground(lipgloss.Color(ColorPrimary))

	var b strings.Builder
	for i := 1; i <= total; i++ {
		switch {
		case i < current:
			b.WriteString(done.Render(GlyphStepDone))
		case i == current:
			b.WriteString(active.Render(GlyphStepDone))
		default:
			b.WriteString(DimStyle().Render(GlyphStepPending))
		}
	}
	return b.String()
}

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

// MarkNote returns the ↳ shown on rows reporting a consequence the reader could
// not have predicted. It takes the accent color rather than a status color: a
// note is neither success nor a problem, it is something to know.
func MarkNote() string {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(ColorAccent)).Render(GlyphNote)
}
