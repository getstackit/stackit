package tui

import (
	"fmt"
	"strings"

	"github.com/getstackit/stackit/internal/tui/style"
)

// FlattenPlannedMove represents a single branch move in the flatten plan.
// This is a local struct to avoid import cycles with the actions/flatten package.
type FlattenPlannedMove struct {
	Branch    string // Branch being moved
	OldParent string // Current parent branch
	NewParent string // Target parent branch (closer to trunk)
}

// FlattenExcludedBranch represents a branch that was kept in place due to code dependencies.
// This is a local struct to avoid import cycles with the actions/flatten package.
type FlattenExcludedBranch struct {
	Branch string // Branch name
	Reason string // Why it was kept in place
}

// FlattenPreviewData contains the data needed to render a flatten preview.
// This is a local struct to avoid import cycles with the actions/flatten package.
type FlattenPreviewData struct {
	Moves            []FlattenPlannedMove    // Branches that will be moved
	UnchangedCount   int                     // Number of branches that won't change
	ExcludedBranches []FlattenExcludedBranch // Branches excluded due to conflicts
}

// truncateBranchName truncates a branch name to maxLen chars, adding ellipsis if needed.
func truncateBranchName(name string, maxLen int) string {
	if len(name) <= maxLen {
		return name
	}
	if maxLen <= 3 {
		return name[:maxLen]
	}
	return name[:maxLen-3] + "..."
}

// RenderFlattenPreview renders a flatten preview showing:
// - List of branches that will be moved and their new parents
// - Count of branches that will stay unchanged
// - Conflict status
func RenderFlattenPreview(preview FlattenPreviewData) string {
	var sb strings.Builder

	// Group moves by destination
	if len(preview.Moves) > 0 {
		movesByDest := make(map[string][]FlattenPlannedMove)
		destOrder := []string{}
		for _, move := range preview.Moves {
			if _, exists := movesByDest[move.NewParent]; !exists {
				destOrder = append(destOrder, move.NewParent)
			}
			movesByDest[move.NewParent] = append(movesByDest[move.NewParent], move)
		}

		fmt.Fprintf(&sb, "Moving %d branch", len(preview.Moves))
		if len(preview.Moves) != 1 {
			sb.WriteString("es")
		}
		sb.WriteString(":\n\n")

		const maxBranchLen = 50

		for _, dest := range destOrder {
			moves := movesByDest[dest]
			// Show destination header
			fmt.Fprintf(&sb, "  %s %s\n", style.ColorGreen("→"), style.ColorGreen(truncateBranchName(dest, maxBranchLen)))

			for _, move := range moves {
				truncatedBranch := truncateBranchName(move.Branch, maxBranchLen)
				fmt.Fprintf(&sb, "      %s\n",
					style.ColorBranchName(truncatedBranch, false))
			}
			sb.WriteString("\n")
		}
	}

	// Unchanged count
	if preview.UnchangedCount > 0 {
		fmt.Fprintf(&sb, "Unchanged: %d branch", preview.UnchangedCount)
		if preview.UnchangedCount != 1 {
			sb.WriteString("es")
		}
		sb.WriteString(" " + style.ColorDim("(already on trunk or dependencies prevent moving)") + "\n\n")
	}

	// Branches kept in place due to code dependencies
	if len(preview.ExcludedBranches) > 0 {
		fmt.Fprintf(&sb, "Kept in stack: %d branch", len(preview.ExcludedBranches))
		if len(preview.ExcludedBranches) != 1 {
			sb.WriteString("es")
		}
		sb.WriteString(" " + style.ColorDim("(have dependent branches)") + "\n")
		for _, excluded := range preview.ExcludedBranches {
			fmt.Fprintf(&sb, "  %s %s %s\n",
				style.ColorDim("·"),
				style.ColorBranchName(excluded.Branch, false),
				style.ColorDim("("+excluded.Reason+")"))
		}
		sb.WriteString("\n")
	}

	// Status summary
	switch {
	case len(preview.Moves) == 0 && len(preview.ExcludedBranches) > 0:
		sb.WriteString(style.ColorDim("No branches can be flattened (all have dependent branches)") + "\n")
	case len(preview.Moves) > 0:
		sb.WriteString(style.ColorGreen("✓ All moves validated") + "\n")
	default:
		sb.WriteString(style.ColorGreen("✓ Stack is already flat") + "\n")
	}

	return sb.String()
}
