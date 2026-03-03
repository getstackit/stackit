package style

import (
	"strings"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/styles"
)

// RenderMarkdown renders markdown content for terminal display.
// If rendering fails, it returns the original content as a fallback.
func RenderMarkdown(content string) string {
	if content == "" {
		return ""
	}

	// Start with dark style and remove document margin for tighter formatting
	customStyle := styles.DarkStyleConfig
	customStyle.Document.Margin = uintPtr(0)

	// Use dark style for consistent formatting - WithAutoStyle() falls back to
	// plain text when terminal detection fails (common in non-interactive contexts)
	r, err := glamour.NewTermRenderer(
		glamour.WithStyles(customStyle),
		glamour.WithWordWrap(80),
	)
	if err != nil {
		// Fallback to plain text on renderer creation failure
		return content
	}

	rendered, err := r.Render(content)
	if err != nil {
		// Fallback to plain text on render failure
		return content
	}

	return strings.TrimSpace(rendered)
}

//go:fix inline
func uintPtr(v uint) *uint {
	return new(v)
}
