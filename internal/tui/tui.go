// Package tui provides the terminal user interface for stackit.
//
// It handles:
//   - Interactive prompts and selections (using survey and bubbletea)
//   - Terminal styling and colors (using lipgloss)
//   - Progress indicators and UI components
package tui

import (
	"github.com/getstackit/stackit/internal/tui/core"
	"github.com/getstackit/stackit/internal/utils"
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

// IsTTY returns true if we can use a TTY for interactive TUI.
// Re-export from utils for backward compatibility.
func IsTTY() bool {
	return utils.IsTTY()
}

// SetInteractive sets whether the TUI should be interactive.
// Re-export from utils for backward compatibility.
// This function is thread-safe and can be called from concurrent goroutines.
func SetInteractive(interactive bool) {
	utils.SetInteractive(interactive)
}
