package actions

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/getstackit/stackit/internal/actions/validation"
	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/output"
	"github.com/getstackit/stackit/internal/tui"
)

// ReorderAction performs the reorder operation
func ReorderAction(ctx *app.Context) error {
	eng := ctx.Engine
	out := ctx.Output
	gctx := ctx.Context

	out.Debug("reorder: starting reorder action")

	// Pre-checks using validation chain
	if err := (validation.Chain{
		validation.MustBeOnBranch(eng),
		validation.MustNotHaveRebaseInProgress(gctx, eng),
		validation.MustNotHaveUncommittedChanges(gctx, eng),
		validation.CurrentBranchMustNotBeTrunk(eng, "reorder"),
	}).Validate(); err != nil {
		out.Debug("reorder: validation failed: %v", err)
		return err
	}

	currentBranch := eng.CurrentBranch().GetName()
	currentBranchObj := eng.GetBranch(currentBranch)
	out.Debug("reorder: current branch object: name=%s, isTrunk=%v, isTracked=%v",
		currentBranchObj.GetName(), currentBranchObj.IsTrunk(), currentBranchObj.IsTracked())

	// Build StackGraph for efficient traversals
	out.Debug("reorder: building stack graph")
	graph := eng.Graph(engine.SortStrategyAlphabetical)
	out.Debug("reorder: stack graph built successfully")

	// Collect branches: get ancestors from trunk to current branch
	out.Debug("reorder: collecting branches from trunk to current branch")
	stack := graph.Downstack(eng.GetBranch(currentBranch), true)
	out.Debug("reorder: found %d branches in stack (including trunk)", len(stack))
	for i, b := range stack {
		out.Debug("reorder: stack[%d]: name=%s, isTrunk=%v, isTracked=%v",
			i, b.GetName(), b.IsTrunk(), b.IsTracked())
	}

	// Filter out trunk and get only tracked branches
	branches := []string{}
	for _, branch := range stack {
		if !branch.IsTrunk() && branch.IsTracked() {
			if err := branch.EnsureCanModify(); err != nil {
				out.Debug("reorder: branch %s cannot be modified: %v", branch.GetName(), err)
				return fmt.Errorf("cannot reorder stack: %w", err)
			}
			branches = append(branches, branch.GetName())
		} else {
			out.Debug("reorder: skipping branch %s (isTrunk=%v, isTracked=%v)",
				branch.GetName(), branch.IsTrunk(), branch.IsTracked())
		}
	}
	if err := EnsureCanModifyNamesHere(ctx, branches...); err != nil {
		return fmt.Errorf("cannot reorder stack: %w", err)
	}
	out.Debug("reorder: filtered to %d reorderable branches: %v", len(branches), branches)

	// Minimum requirements: need at least 2 branches to reorder
	if len(branches) < 2 {
		out.Debug("reorder: insufficient branches for reordering (found %d)", len(branches))
		return fmt.Errorf("need at least 2 branches to reorder. Found %d branch(es)", len(branches))
	}

	// Store original order for comparison (in trunk-to-tip order)
	originalOrder := make([]string, len(branches))
	copy(originalOrder, branches)
	out.Debug("reorder: original order (trunk-to-tip): %v", originalOrder)

	// Get trunk name for TUI display
	trunkName := eng.Trunk().GetName()

	// Reverse branches for TUI display (tip first, then down to trunk)
	// This makes the UI more intuitive: top of stack at the top of the list
	displayBranches := make([]string, len(branches))
	copy(displayBranches, branches)
	slices.Reverse(displayBranches)
	out.Debug("reorder: display order (tip-first): %v", displayBranches)

	// Open TUI or Editor to get new order
	var newOrder []string
	isTTY := tui.IsTTY()
	out.Debug("reorder: isTTY=%v", isTTY)
	if isTTY {
		out.Debug("reorder: opening TUI for reordering")
		var err error
		newOrder, err = tui.RunReorderTUI(displayBranches, trunkName)
		if err != nil {
			if err.Error() == "reorder canceled" {
				out.Debug("reorder: user canceled reorder via TUI")
				out.Info("Reorder canceled.")
				return nil
			}
			out.Debug("reorder: TUI failed: %v", err)
			return fmt.Errorf("TUI failed: %w", err)
		}
		out.Debug("reorder: TUI returned new order (tip-first): %v", newOrder)
		// Reverse back to trunk-to-tip order for parent relationship updates
		slices.Reverse(newOrder)
		out.Debug("reorder: converted to trunk-to-tip order: %v", newOrder)
	} else {
		out.Debug("reorder: opening editor for reordering (non-TTY mode)")
		// Create editor content with instructions
		editorContent := buildEditorContent(branches)
		out.Debug("reorder: editor content prepared (%d bytes)", len(editorContent))

		// Open editor
		editedContent, err := tui.OpenEditor(editorContent, "stackit-reorder-*.txt")
		if err != nil {
			out.Debug("reorder: failed to open editor: %v", err)
			return fmt.Errorf("failed to open editor: %w", err)
		}
		out.Debug("reorder: editor returned content (%d bytes)", len(editedContent))

		// Parse and validate edited content
		newOrder, err = parseEditorContent(editedContent, originalOrder)
		if err != nil {
			out.Debug("reorder: failed to parse editor content: %v", err)
			return err
		}
		out.Debug("reorder: parsed new order from editor: %v", newOrder)
	}

	// Check if order actually changed
	out.Debug("reorder: comparing original and new order")
	out.Debug("reorder: original: %v", originalOrder)
	out.Debug("reorder: new:      %v", newOrder)
	if slices.Equal(originalOrder, newOrder) {
		out.Debug("reorder: order unchanged, no action needed")
		out.Info("Branch order unchanged. No action taken.")
		return nil
	}
	out.Debug("reorder: order changed, proceeding with reorder")

	// Update parent relationships
	out.Debug("reorder: updating parent relationships")
	if err := updateParentRelationships(gctx, eng, newOrder, out); err != nil {
		out.Debug("reorder: failed to update parent relationships: %v", err)
		return fmt.Errorf("failed to update parent relationships: %w", err)
	}
	out.Debug("reorder: parent relationships updated successfully")

	// Identify affected branches: find the first branch that moved or changed parent
	out.Debug("reorder: finding first affected branch")
	firstAffectedBranchName := findFirstAffectedBranch(eng, originalOrder, newOrder, out)
	out.Debug("reorder: first affected branch: %s", firstAffectedBranchName)

	// Get all affected branches (first affected and all its descendants)
	out.Debug("reorder: getting all affected branches (including descendants)")
	affectedBranches := graph.Range(eng.GetBranch(firstAffectedBranchName), engine.StackRange{
		RecursiveChildren: true,
		IncludeCurrent:    true,
		RecursiveParents:  false,
	})
	out.Debug("reorder: found %d affected branches to restack", len(affectedBranches))
	for i, b := range affectedBranches {
		out.Debug("reorder: affected[%d]: %s", i, b.GetName())
	}

	// Restack all affected branches
	out.Debug("reorder: starting restack of affected branches")
	if err := RestackBranches(ctx, affectedBranches); err != nil {
		out.Debug("reorder: failed to restack branches: %v", err)
		return fmt.Errorf("failed to restack branches: %w", err)
	}
	out.Debug("reorder: restack completed successfully")

	out.Info("Reordered and restacked branches.")
	out.Debug("reorder: reorder action completed successfully")
	return nil
}

// buildEditorContent creates the initial editor content with instructions
func buildEditorContent(branches []string) string {
	var sb strings.Builder
	sb.WriteString("# Reorder branches by moving lines up or down.\n")
	sb.WriteString("# Lines starting with '#' are ignored.\n")
	sb.WriteString("# Do not add or remove branches.\n")
	sb.WriteString("# Save and close to apply changes.\n")
	sb.WriteString("#\n")
	for _, branch := range branches {
		sb.WriteString(branch)
		sb.WriteString("\n")
	}
	return sb.String()
}

// parseEditorContent parses the edited content and validates it
func parseEditorContent(content string, originalBranches []string) ([]string, error) {
	lines := strings.Split(content, "\n")
	branches := []string{}

	// Create a set of original branches for validation
	originalSet := make(map[string]bool)
	for _, b := range originalBranches {
		originalSet[b] = true
	}

	// Parse lines, ignoring comments and empty lines
	for _, line := range lines {
		line = strings.TrimSpace(line)
		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		branches = append(branches, line)
	}

	// Validate: check for duplicates
	seen := make(map[string]bool)
	for _, branch := range branches {
		if seen[branch] {
			return nil, fmt.Errorf("duplicate branch found: %s", branch)
		}
		seen[branch] = true
	}

	// Validate: check that all original branches are present
	for _, original := range originalBranches {
		if !seen[original] {
			return nil, fmt.Errorf("branch %s was removed. Use 'stackit delete' to remove branches", original)
		}
	}

	// Validate: check that no new branches were added
	if len(branches) != len(originalBranches) {
		return nil, fmt.Errorf("number of branches changed. Expected %d, got %d", len(originalBranches), len(branches))
	}

	// Validate: check that all branches are valid (exist in original set)
	for _, branch := range branches {
		if !originalSet[branch] {
			return nil, fmt.Errorf("unknown branch: %s", branch)
		}
	}

	return branches, nil
}

// updateParentRelationships updates the parent of each branch in the new order
func updateParentRelationships(ctx context.Context, eng reorderUpdateEngine, newOrder []string, out output.Output) error {
	out.Debug("reorder: updateParentRelationships: processing %d branches", len(newOrder))

	// Set parent of first branch to trunk, then chain each subsequent branch
	trunk := eng.Trunk()
	out.Debug("reorder: updateParentRelationships: trunk is %s", trunk.GetName())

	moves := make([]engine.BranchParentMove, len(newOrder))
	for i, branchName := range newOrder {
		parentName := trunk.GetName()
		if i > 0 {
			parentName = newOrder[i-1]
		}
		out.Debug("reorder: updateParentRelationships: setting parent of %s to %s",
			branchName, parentName)
		moves[i] = engine.BranchParentMove{Branch: branchName, NewParent: parentName}
	}

	if err := eng.ReparentBranchesToParents(ctx, moves); err != nil {
		return fmt.Errorf("failed to update reordered parent relationships: %w", err)
	}

	out.Debug("reorder: updateParentRelationships: all parent relationships updated")
	return nil
}

type reorderUpdateEngine interface {
	engine.StackNavigator
	engine.BranchTracking
}

// findFirstAffectedBranch finds the first branch that moved or changed parent
func findFirstAffectedBranch(eng engine.StackNavigator, originalOrder, newOrder []string, out output.Output) string {
	out.Debug("reorder: findFirstAffectedBranch: comparing %d branches", len(newOrder))

	trunk := eng.Trunk().GetName()
	out.Debug("reorder: findFirstAffectedBranch: trunk is %s", trunk)

	// Create a map of original positions
	originalPos := make(map[string]int)
	for i, branch := range originalOrder {
		originalPos[branch] = i
	}
	out.Debug("reorder: findFirstAffectedBranch: original positions: %v", originalPos)

	// Find the first branch that moved or has a different parent
	for i, branch := range newOrder {
		out.Debug("reorder: findFirstAffectedBranch: checking branch %s at new position %d (was at %d)",
			branch, i, originalPos[branch])

		// Check if position changed
		if originalPos[branch] != i {
			out.Debug("reorder: findFirstAffectedBranch: %s moved from position %d to %d",
				branch, originalPos[branch], i)
			return branch
		}

		// Check if parent changed
		var expectedParent string
		if i == 0 {
			expectedParent = trunk
		} else {
			expectedParent = newOrder[i-1]
		}

		branchObj := eng.GetBranch(branch)
		currentParent := branchObj.GetParent()
		currentParentName := ""
		if currentParent == nil {
			currentParentName = trunk
		} else {
			currentParentName = currentParent.GetName()
		}

		out.Debug("reorder: findFirstAffectedBranch: %s current parent=%s, expected parent=%s",
			branch, currentParentName, expectedParent)

		if currentParentName != expectedParent {
			out.Debug("reorder: findFirstAffectedBranch: %s has different parent (current=%s, expected=%s)",
				branch, currentParentName, expectedParent)
			return branch
		}
	}

	// Fallback: return first branch if no changes detected (shouldn't happen)
	out.Debug("reorder: findFirstAffectedBranch: no specific affected branch found, using fallback")
	if len(newOrder) > 0 {
		out.Debug("reorder: findFirstAffectedBranch: returning first branch as fallback: %s", newOrder[0])
		return newOrder[0]
	}
	out.Debug("reorder: findFirstAffectedBranch: no branches to return")
	return ""
}
