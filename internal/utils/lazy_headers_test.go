package utils

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLazyHeaders_CommitsOnceOnFirstItem(t *testing.T) {
	t.Parallel()
	h := NewLazyHeaders[string]()
	h.Start("trunk")

	assert.False(t, h.Committed("trunk"))
	assert.False(t, h.Any())

	// First item commits the header, with no separator (nothing before it).
	emit, separate := h.CommitOnItem("trunk")
	assert.True(t, emit, "first item should commit the header")
	assert.False(t, separate, "first header needs no separator")
	assert.True(t, h.Committed("trunk"))
	assert.True(t, h.Any())

	// Subsequent items for the same section never re-emit the header.
	emit, separate = h.CommitOnItem("trunk")
	assert.False(t, emit)
	assert.False(t, separate)
}

func TestLazyHeaders_SeparatesSuccessiveSections(t *testing.T) {
	t.Parallel()
	h := NewLazyHeaders[string]()
	h.Start("trunk")
	h.Start("github")

	emit, separate := h.CommitOnItem("trunk")
	assert.True(t, emit)
	assert.False(t, separate, "first committed header has no separator")

	// The second section's header is separated from the first by a blank line.
	emit, separate = h.CommitOnItem("github")
	assert.True(t, emit)
	assert.True(t, separate, "later headers are separated from the previous group")
}

func TestLazyHeaders_UnstartedSectionStaysSilent(t *testing.T) {
	t.Parallel()
	h := NewLazyHeaders[string]()

	// An item for a section that never started (e.g. standalone restack drives
	// items without starting a phase) must not emit a header.
	emit, separate := h.CommitOnItem("restack")
	assert.False(t, emit)
	assert.False(t, separate)
	assert.False(t, h.Committed("restack"))
	assert.False(t, h.Any())
}

func TestLazyHeaders_StartedButNoItemStaysSilent(t *testing.T) {
	t.Parallel()
	h := NewLazyHeaders[string]()
	h.Start("branches")
	h.Start("clean")

	// Phases that start but never produce an item commit nothing.
	assert.False(t, h.Committed("branches"))
	assert.False(t, h.Committed("clean"))
	assert.False(t, h.Any())
}
