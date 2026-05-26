package describe

import (
	"fmt"
	"strings"

	"github.com/getstackit/stackit/internal/actions"
	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/errors"
	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/internal/tui"
	"github.com/getstackit/stackit/internal/tui/style"
	"github.com/getstackit/stackit/internal/utils"
)

// Options contains options for the describe command
type Options struct {
	Title       string // Set title directly (non-interactive)
	Description string // Set description directly (non-interactive)
	Clear       bool   // Remove the stack description
	Show        bool   // Display the current description
}

// Action implements the stackit describe command
func Action(ctx *app.Context, opts Options, handler Handler) error {
	if handler == nil {
		handler = &NullHandler{}
	}
	defer handler.Cleanup()

	eng := ctx.Engine
	out := ctx.Output

	// Get current branch
	currentBranch := eng.CurrentBranch()
	if currentBranch == nil {
		return errors.ErrNotOnBranch
	}

	// Check if on trunk
	if eng.IsTrunk(*currentBranch) {
		return fmt.Errorf("cannot set stack description on trunk")
	}

	// Check if branch is tracked
	if !currentBranch.IsTracked() {
		return fmt.Errorf("branch %s is not tracked; use 'stackit track' first", currentBranch.GetName())
	}

	// Get stack root for display
	stackRoot := eng.GetStackRootForBranch(*currentBranch)
	if stackRoot == "" {
		return fmt.Errorf("branch %s is not part of a tracked stack", currentBranch.GetName())
	}

	// Handle --show
	if opts.Show {
		return showDescription(ctx, *currentBranch, stackRoot)
	}

	// Handle --clear
	if opts.Clear {
		if err := eng.ClearStackDescription(ctx.Context, *currentBranch); err != nil {
			return fmt.Errorf("failed to clear stack description: %w", err)
		}
		out.Info("Cleared stack description for stack rooted at %s.", style.ColorBranchName(stackRoot, false))

		// Mark stack for PR updates and push metadata
		if err := markStackAndPushMetadata(ctx, eng, stackRoot); err != nil {
			out.Debug("Failed to push metadata changes: %v", err)
		}
		return nil
	}

	// Handle non-interactive mode with -m flag
	if opts.Title != "" {
		desc := &git.StackDescription{
			Title:       opts.Title,
			Description: opts.Description,
		}
		return applyStackDescription(ctx, *currentBranch, stackRoot, desc, opts.Description)
	}

	// Try reading from stdin when not interactive
	if !handler.IsInteractive() {
		stdinContent, err := utils.ReadFromStdin()
		if err == nil && stdinContent != "" {
			if desc := ParseEditorContent(stdinContent); desc != nil {
				return applyStackDescription(ctx, *currentBranch, stackRoot, desc, TruncateDescription(desc.Description, 60))
			}
		}
		return fmt.Errorf("must specify --message or run in interactive mode")
	}

	existingDesc := eng.GetStackDescription(*currentBranch)
	newDesc, err := openEditor(existingDesc)
	if err != nil {
		return err
	}

	if newDesc == nil {
		out.Info("Aborting: at least a title is required. Use -m to set inline.")
		return nil
	}

	return applyStackDescription(ctx, *currentBranch, stackRoot, newDesc, TruncateDescription(newDesc.Description, 60))
}

// applyStackDescription sets the description on the stack root, prints a
// confirmation, and pushes metadata. displayDesc is the (possibly truncated)
// description text shown in the confirmation; pass "" to omit that line.
func applyStackDescription(ctx *app.Context, branch engine.Branch, stackRoot string, desc *git.StackDescription, displayDesc string) error {
	eng := ctx.Engine
	out := ctx.Output

	if err := eng.SetStackDescription(ctx.Context, branch, desc); err != nil {
		return fmt.Errorf("failed to set stack description: %w", err)
	}

	out.Info("Set stack description for stack rooted at %s:", style.ColorBranchName(stackRoot, false))
	out.Info("  Title: %s", style.ColorDim(desc.Title))
	if displayDesc != "" {
		out.Info("  Description: %s", style.ColorDim(displayDesc))
	}

	if err := markStackAndPushMetadata(ctx, eng, stackRoot); err != nil {
		out.Debug("Failed to push metadata changes: %v", err)
	}
	return nil
}

// markStackAndPushMetadata marks all stack branches for PR update and pushes metadata refs.
// Unlike PushMetadataAndSyncPRs, this does NOT immediately update GitHub PRs.
func markStackAndPushMetadata(ctx *app.Context, eng engine.Engine, stackRoot string) error {
	out := ctx.Output

	// Mark all branches in the stack for PR body update
	graph := eng.Graph(engine.SortStrategyAlphabetical)
	root := eng.GetBranch(stackRoot)
	if root.IsTracked() {
		if err := eng.BatchMarkNeedsPRBodyUpdate(graph.CollectBranches(root).Names()); err != nil {
			out.Debug("Failed to mark branches for PR body update: %v", err)
		}
	}

	// Push metadata refs (fast git operation)
	return actions.PushMetadataOnly(ctx, eng, []string{stackRoot})
}

func showDescription(ctx *app.Context, branch engine.Branch, stackRoot string) error { //nolint:unparam // error return for API consistency
	out := ctx.Output
	desc := ctx.Engine.GetStackDescription(branch)

	if desc == nil || desc.IsEmpty() {
		out.Info("Stack rooted at %s has no description set.", style.ColorBranchName(stackRoot, false))
		return nil
	}

	out.Info("Stack description for %s:", style.ColorBranchName(stackRoot, false))
	out.Info("")
	out.Info("  Title: %s", desc.Title)
	if desc.Description != "" {
		out.Info("")
		// Print description with indentation
		for line := range strings.SplitSeq(desc.Description, "\n") {
			out.Info("  %s", line)
		}
	}
	return nil
}

func openEditor(existing *git.StackDescription) (*git.StackDescription, error) {
	template := buildEditorTemplate(existing)

	content, err := tui.OpenEditor(template, "STACK_DESCRIPTION-*")
	if err != nil {
		return nil, fmt.Errorf("editor failed: %w", err)
	}

	return ParseEditorContent(content), nil
}

func buildEditorTemplate(existing *git.StackDescription) string {
	var sb strings.Builder

	if existing != nil && !existing.IsEmpty() {
		sb.WriteString(existing.Title)
		sb.WriteString("\n\n")
		sb.WriteString(existing.Description)
		sb.WriteString("\n")
	}

	sb.WriteString("\n# Stack Description\n")
	sb.WriteString("#\n")
	sb.WriteString("# First line: Title (short summary of the stack)\n")
	sb.WriteString("# Following lines after blank: Description (detailed explanation)\n")
	sb.WriteString("#\n")
	sb.WriteString("# Leave empty to abort. Lines starting with # are ignored.\n")

	return sb.String()
}

// ParseEditorContent parses the editor content into a StackDescription.
// Returns nil if the content is empty (abort).
func ParseEditorContent(content string) *git.StackDescription {
	lines := strings.Split(content, "\n")
	titleLines := make([]string, 0, 5)
	descLines := make([]string, 0, len(lines))
	foundBlank := false
	foundContent := false
	for _, line := range lines {
		// Skip comment lines
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}

		trimmed := strings.TrimSpace(line)

		if !foundContent {
			// Looking for title (first non-empty, non-comment line)
			if trimmed != "" {
				titleLines = append(titleLines, line)
				foundContent = true
			}
			continue
		}

		if !foundBlank {
			// Looking for blank line that separates title from description
			if trimmed == "" {
				foundBlank = true
			} else {
				// Multi-line title (append to title)
				titleLines = append(titleLines, line)
			}
			continue
		}

		// Everything after blank line is description
		descLines = append(descLines, line)
	}

	if len(titleLines) == 0 {
		return nil // Empty, abort
	}

	title := strings.TrimSpace(strings.Join(titleLines, " "))
	description := strings.TrimSpace(strings.Join(descLines, "\n"))

	return &git.StackDescription{
		Title:       title,
		Description: description,
	}
}

// TruncateDescription truncates a description string for display.
// It returns the first line, truncated to maxLen characters if needed.
func TruncateDescription(s string, maxLen int) string {
	// Get first line or truncate
	lines := strings.Split(s, "\n")
	first := lines[0]
	if len(first) > maxLen {
		if maxLen < 0 || maxLen <= 3 {
			return "..."
		}
		return first[:maxLen-3] + "..."
	}
	if len(lines) > 1 {
		return first + "..."
	}
	return first
}
