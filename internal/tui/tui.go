// Package tui provides the terminal user interface for stackit.
//
// It handles:
//   - Interactive prompts and selections (using survey and bubbletea)
//   - Terminal styling and colors (using lipgloss)
//   - Progress indicators and UI components
package tui

import (
	"github.com/getstackit/stackit/internal/tui/core"
)

// Key constants re-exported from core for backwards compatibility.
const (
	KeyCtrlC = core.KeyCtrlC
	KeyQuit  = core.KeyQuit
	KeyEsc   = core.KeyEsc
	KeyEnter = core.KeyEnter
	KeyUp    = core.KeyUp
	KeyDown  = core.KeyDown
	KeyTab   = core.KeyTab
)
