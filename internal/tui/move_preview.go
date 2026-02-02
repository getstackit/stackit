package tui

import (
	"fmt"
	"strings"

	"stackit.dev/stackit/internal/tui/style"
)

// MovePreviewData contains the data needed to render a move preview.
// This is a local struct to avoid import cycles with the actions/move package.
type MovePreviewData struct {
	SourceBranch   string   // Branch being moved
	OldParent      string   // Current parent branch
	NewParent      string   // Target parent branch
	Commits        []string // Commit subjects that will be moved
	Descendants    []string // Descendant branches that will be restacked
	HasConflicts   bool     // Whether the move will cause conflicts
	ConflictBranch string   // Which branch would have conflicts (if any)
	ConflictError  string   // Error message describing the conflict
}

// RenderMovePreviewSimple renders a simplified move preview showing:
// - Current location (dimmed)
// - New location (highlighted)
// - Conflict status
func RenderMovePreviewSimple(preview MovePreviewData) string {
	var sb strings.Builder

	// Header
	sb.WriteString(style.ColorDim("─────────────────────────────────────") + "\n")
	sb.WriteString("Move Preview:\n")
	sb.WriteString(style.ColorDim("─────────────────────────────────────") + "\n")
	sb.WriteString("\n")

	// Current position (dimmed)
	sb.WriteString(style.ColorDim("Current position:") + "\n")
	sb.WriteString(fmt.Sprintf("  %s → %s %s\n",
		style.ColorDim(preview.OldParent),
		style.ColorDim(preview.SourceBranch),
		style.ColorDim("(moving from here)")))
	sb.WriteString("\n")

	// New position (highlighted)
	sb.WriteString("New position:\n")
	sb.WriteString(fmt.Sprintf("  %s → %s %s\n",
		style.ColorBranchName(preview.NewParent, false),
		style.ColorGreen(preview.SourceBranch),
		style.ColorGreen("(moving to here)")))
	sb.WriteString("\n")

	// Commits being moved
	if len(preview.Commits) > 0 {
		sb.WriteString(fmt.Sprintf("Commits to move (%d):\n", len(preview.Commits)))
		for _, commit := range preview.Commits {
			sb.WriteString(fmt.Sprintf("  • %s\n", commit))
		}
		sb.WriteString("\n")
	}

	// Descendants to restack
	if len(preview.Descendants) > 0 {
		sb.WriteString(fmt.Sprintf("Branches to restack (%d):\n", len(preview.Descendants)))
		for _, desc := range preview.Descendants {
			sb.WriteString(fmt.Sprintf("  • %s\n", style.ColorBranchName(desc, false)))
		}
		sb.WriteString("\n")
	}

	// Conflict status
	sb.WriteString(style.ColorDim("─────────────────────────────────────") + "\n")
	if preview.HasConflicts {
		sb.WriteString(style.ColorRed("✗ ") + style.ColorRed("Conflicts detected") + "\n")
		sb.WriteString(fmt.Sprintf("  Branch: %s\n", style.ColorBranchName(preview.ConflictBranch, false)))
		sb.WriteString(fmt.Sprintf("  Error: %s\n", preview.ConflictError))
	} else {
		sb.WriteString(style.ColorGreen("✓ ") + style.ColorGreen("Move will complete without conflicts") + "\n")
	}
	sb.WriteString(style.ColorDim("─────────────────────────────────────") + "\n")

	return sb.String()
}
