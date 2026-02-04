// Package move provides functionality for moving branches to different parents in the stack.
package move

import (
	"fmt"
	"strings"
	"time"

	"stackit.dev/stackit/internal/actions"
	"stackit.dev/stackit/internal/actions/handler"
	"stackit.dev/stackit/internal/actions/validation"
	"stackit.dev/stackit/internal/app"
	"stackit.dev/stackit/internal/engine"
	"stackit.dev/stackit/internal/git"
	"stackit.dev/stackit/internal/output"
	"stackit.dev/stackit/internal/tui/style"
)

// Options contains options for the move command
type Options struct {
	Source      string // Branch to move (defaults to current branch)
	Onto        string // Branch to move onto
	SkipConfirm bool   // Skip confirmation prompt (--yes flag)
	DryRun      bool   // If true, only shows what would happen without making changes
	AutoRename  bool   // Auto-rename branch when scope changes (non-interactive mode)
}

// Action performs the move operation
func Action(ctx *app.Context, opts Options, h Handler) error {
	eng := ctx.Engine
	out := ctx.Output
	gctx := ctx.Context

	// Use null handler if none provided
	if h == nil {
		h = &NullHandler{}
	}
	defer h.Cleanup()

	graph := engine.BuildStackGraph(eng, engine.SortStrategyAlphabetical, nil)

	// Default source to current branch
	source := opts.Source
	if source == "" {
		currentBranch := eng.CurrentBranch()
		if currentBranch == nil {
			return fmt.Errorf("not on a branch and no source branch specified")
		}
		source = currentBranch.GetName()
	}

	// Take snapshot before modifying the repository
	snapshotOpts := actions.NewSnapshot("move",
		actions.WithFlagValue("--source", opts.Source),
		actions.WithFlagValue("--onto", opts.Onto),
	)
	if err := eng.TakeSnapshot(snapshotOpts); err != nil {
		// Log but don't fail - snapshot is best effort
		out.Debug("Failed to take snapshot: %v", err)
	}

	// Validate source branch
	if err := validation.ValidateSourceBranch(eng, source, "move"); err != nil {
		return err
	}

	// Validate target branch
	onto := opts.Onto
	if err := validation.ValidateTargetBranch(eng, source, onto, "move"); err != nil {
		return err
	}

	sourceBranch := eng.GetBranch(source)
	ontoBranch := eng.GetBranch(onto)

	// Cycle detection: ensure onto is not a descendant of source
	if graph.IsDescendant(sourceBranch, onto) {
		return fmt.Errorf("cannot move %s onto its own descendant %s", source, onto)
	}

	// Get descendants for rebase validation
	descendants := graph.Range(sourceBranch, engine.StackRange{
		RecursiveChildren: true,
		IncludeCurrent:    true,
		RecursiveParents:  false,
	})

	// Get current parent for preview
	oldParent := sourceBranch.GetParent()
	oldParentName := sourceBranch.GetParentPrecondition()

	// Capture old divergence point to preserve it after reparenting
	// This ensures we only move the commits that belong to this branch.
	oldParentRev, _ := eng.GetDivergencePoint(source)

	// Build rebase specs for validation (needed for both preview conflict detection and actual move)
	rebaseSpecs := BuildRebaseSpecs(eng, out, source, onto, oldParent, oldParentRev, descendants)

	// Dry-run mode: validate and print what would happen without making changes
	if opts.DryRun {
		return dryRun(ctx, source, oldParentName, onto, sourceBranch, descendants, rebaseSpecs)
	}

	// Prompt for confirmation in interactive mode (unless --yes flag is set)
	if h.IsInteractive() && !opts.SkipConfirm {
		// Validate rebases BEFORE showing preview so user can see conflict info
		validation, validationErr := eng.ValidateRebases(gctx, rebaseSpecs)

		// Get commits that will be moved
		commits, _ := eng.GetAllCommits(sourceBranch, engine.CommitFormatSubject)

		// Get descendant names (excluding source itself)
		var descendantNames []string
		for _, d := range descendants {
			if d.GetName() != source {
				descendantNames = append(descendantNames, d.GetName())
			}
		}

		preview := Preview{
			SourceBranch: source,
			OldParent:    oldParentName,
			NewParent:    onto,
			Commits:      commits,
			Descendants:  descendantNames,
		}

		// Add conflict info to preview if validation succeeded (even if there are conflicts)
		if validationErr == nil && !validation.Success {
			preview.HasConflicts = true
			preview.ConflictBranch = validation.FailedBranch
			preview.ConflictError = validation.ErrorMessage
			preview.ConflictingFiles = validation.ConflictingFiles
		}

		confirmed, err := h.PromptConfirmMove(preview)
		if err != nil {
			return fmt.Errorf("failed to prompt for confirmation: %w", err)
		}
		if !confirmed {
			out.Info("Move canceled.")
			return nil
		}

		// If validation itself failed (not conflicts, but actual error), return error now
		if validationErr != nil {
			return fmt.Errorf("failed to validate rebases: %w", validationErr)
		}

		// If there are conflicts, return error after user has seen the preview
		if preview.HasConflicts {
			return fmt.Errorf("move would cause conflicts: %s on branch %s", preview.ConflictError, preview.ConflictBranch)
		}
	} else {
		// Non-interactive mode: validate and fail immediately if there are conflicts
		h.OnStep(StepValidating, handler.StatusStarted, "Validating rebases...")
		validation, err := eng.ValidateRebases(gctx, rebaseSpecs)
		if err != nil {
			h.OnStep(StepValidating, handler.StatusFailed, err.Error())
			return fmt.Errorf("failed to validate rebases: %w", err)
		}
		if !validation.Success {
			h.OnStep(StepValidating, handler.StatusFailed, validation.ErrorMessage)
			return fmt.Errorf("move would cause conflicts: %s on branch %s", validation.ErrorMessage, validation.FailedBranch)
		}
		h.OnStep(StepValidating, handler.StatusCompleted, "Validation passed")
	}

	// Check for scope change and potential rename
	var renamed bool
	sourceScope := sourceBranch.GetScope()
	ontoScope := ontoBranch.GetScope()
	if sourceScope.IsDefined() && ontoScope.IsDefined() && !sourceScope.Equal(ontoScope) {
		shouldRename := false
		if h.IsInteractive() && strings.Contains(source, sourceScope.String()) {
			confirmed, err := h.PromptRename(source, sourceScope.String(), ontoScope.String())
			if err == nil && confirmed {
				shouldRename = true
			}
		} else if opts.AutoRename && strings.Contains(source, sourceScope.String()) {
			shouldRename = true
		}

		if shouldRename {
			newName := strings.Replace(source, sourceScope.String(), ontoScope.String(), 1)
			if err := eng.RenameBranch(gctx, eng.GetBranch(source), eng.GetBranch(newName)); err != nil {
				out.Info("Warning: failed to rename branch: %v", err)
			} else {
				h.OnRename(source, newName)
				out.Info("Renamed branch %s to %s.", style.ColorBranchName(source, false), style.ColorBranchName(newName, true))
				source = newName
				sourceBranch = eng.GetBranch(source)
				renamed = true
			}
		}
	}

	// Start handler with branch info (oldParentName computed earlier for preview)
	h.Start(source, oldParentName, onto)

	// Update parent in engine, preserving the divergence point
	if err := eng.SetParentPreservingDivergence(gctx, sourceBranch, ontoBranch, oldParentRev); err != nil {
		return fmt.Errorf("failed to set parent: %w", err)
	}

	// Update stack IDs if moving to a different stack
	oldStackID := eng.GetStackID(sourceBranch)
	newStackID := eng.GetStackID(ontoBranch)

	// Determine what stack ID the source should have after the move
	var targetStackID string
	if eng.IsTrunk(ontoBranch) {
		// Moving to trunk creates a new stack
		targetStackID = eng.GenerateStackID(source)
		// Create a new stack ref with proper metadata
		stackMeta := &git.StackMeta{
			ID:        targetStackID,
			CreatedAt: time.Now(),
		}
		if err := eng.CreateStackRef(targetStackID, stackMeta); err != nil {
			out.Debug("Failed to create stack ref: %v", err)
		}
	} else if newStackID != "" && newStackID != oldStackID {
		// Moving to a different stack - inherit that stack's ID
		targetStackID = newStackID
	}

	// Update stack IDs if needed
	if targetStackID != "" {
		// Update source branch
		if err := eng.SetStackID(gctx, sourceBranch, targetStackID); err != nil {
			out.Debug("Failed to update stack ID for %s: %v", source, err)
		}
		// Update all descendants
		for _, d := range descendants {
			if d.GetName() != source {
				if err := eng.SetStackID(gctx, d, targetStackID); err != nil {
					out.Debug("Failed to update stack ID for %s: %v", d.GetName(), err)
				}
			}
		}
	}

	// Rebuild graph after parent change for downstream traversals
	graph = engine.BuildStackGraph(eng, engine.SortStrategyAlphabetical, nil)

	out.Info("Moved %s from %s to %s.",
		style.ColorBranchName(source, true),
		style.ColorBranchName(oldParentName, false),
		style.ColorBranchName(onto, false))

	// Get all branches that need restacking: source and all its descendants
	branchesToRestack := graph.Range(sourceBranch, engine.StackRange{
		RecursiveChildren: true,
		IncludeCurrent:    true,
		RecursiveParents:  false,
	})

	// Restack source and all its descendants
	if err := actions.RestackBranches(ctx, branchesToRestack); err != nil {
		return fmt.Errorf("failed to restack branches: %w", err)
	}

	// Mark affected branches as needing PR body update during next sync
	affectedBranches := []string{source}
	if oldParentName != eng.Trunk().GetName() {
		affectedBranches = append(affectedBranches, oldParentName)
	}
	for _, branchName := range affectedBranches {
		if err := eng.MarkNeedsPRBodyUpdate(branchName); err != nil {
			out.Debug("Failed to mark %s for PR body update: %v", branchName, err)
		}
	}

	newName := ""
	if renamed {
		newName = source
	}
	h.Complete(Result{
		SourceBranch: source,
		OldParent:    oldParentName,
		NewParent:    onto,
		Renamed:      renamed,
		NewName:      newName,
	})
	return nil
}

// dryRun validates and prints what the move would do without making changes.
func dryRun(ctx *app.Context, source, oldParentName, onto string, sourceBranch engine.Branch, descendants []engine.Branch, rebaseSpecs []engine.RebaseSpec) error {
	eng := ctx.Engine
	out := ctx.Output
	gctx := ctx.Context

	// Run validation
	validation, validationErr := eng.ValidateRebases(gctx, rebaseSpecs)

	// Get commits that will be moved
	commits, _ := eng.GetAllCommits(sourceBranch, engine.CommitFormatSubject)

	// Get descendant names (excluding source itself)
	var descendantNames []string
	for _, d := range descendants {
		if d.GetName() != source {
			descendantNames = append(descendantNames, d.GetName())
		}
	}

	// Print dry-run header
	out.Info("Dry-run: showing what would happen without making changes\n")

	// Print move summary
	out.Info("Move: %s", style.ColorBranchName(source, true))
	out.Info("  From: %s", style.ColorBranchName(oldParentName, false))
	out.Info("  To:   %s", style.ColorBranchName(onto, false))

	// Print commits that would be moved
	if len(commits) > 0 {
		out.Info("\nCommits to move (%d):", len(commits))
		for _, c := range commits {
			out.Info("  • %s", c)
		}
	} else {
		out.Info("\nNo commits to move (branch has no commits)")
	}

	// Print descendants that would be restacked
	if len(descendantNames) > 0 {
		out.Info("\nDescendant branches to restack (%d):", len(descendantNames))
		for _, name := range descendantNames {
			out.Info("  • %s", style.ColorBranchName(name, false))
		}
	}

	// Print validation result
	out.Info("")
	if validationErr != nil {
		out.Info("Validation: %s", style.ColorRed("failed"))
		out.Info("  Error: %s", validationErr.Error())
		return fmt.Errorf("validation failed: %w", validationErr)
	}

	if !validation.Success {
		out.Info("Validation: %s", style.ColorRed("conflicts detected"))
		out.Info("  Branch: %s", style.ColorBranchName(validation.FailedBranch, false))
		out.Info("  Error: %s", validation.ErrorMessage)
		if len(validation.ConflictingFiles) > 0 {
			out.Info("  Conflicting files:")
			for _, file := range validation.ConflictingFiles {
				out.Info("    - %s", file)
			}
		}
		return fmt.Errorf("move would cause conflicts: %s on branch %s", validation.ErrorMessage, validation.FailedBranch)
	}

	out.Info("Validation: %s", style.ColorGreen("passed"))
	out.Info("\nRun without --dry-run to execute the move.")
	return nil
}

// BuildRebaseSpecs builds the rebase specifications for validating/executing the move.
// Exported so it can be used by the selection validation callback.
func BuildRebaseSpecs(eng engine.Engine, out output.Output, source, onto string, oldParent *engine.Branch, oldParentRev string, descendants []engine.Branch) []engine.RebaseSpec {
	rebaseSpecs := make([]engine.RebaseSpec, 0, len(descendants))

	ontoBranch := eng.GetBranch(onto)

	// Get the target parent's revision for the source branch rebase
	ontoRev, err := eng.GetRevision(ontoBranch)
	if err != nil {
		out.Debug("Failed to get revision for %s: %v", onto, err)
	}

	// Source branch: rebase onto new parent
	sourceOldUpstream := oldParentRev
	if sourceOldUpstream == "" {
		// Fallback to current parent's revision when metadata doesn't have it
		if oldParent != nil {
			var revErr error
			sourceOldUpstream, revErr = eng.GetRevision(*oldParent)
			if revErr != nil {
				out.Debug("Failed to get revision for old parent %s: %v", oldParent.GetName(), revErr)
			}
		} else {
			var revErr error
			sourceOldUpstream, revErr = eng.GetRevision(eng.Trunk())
			if revErr != nil {
				out.Debug("Failed to get revision for trunk: %v", revErr)
			}
		}
	}
	rebaseSpecs = append(rebaseSpecs, engine.RebaseSpec{
		Branch:      source,
		NewParent:   ontoRev,
		OldUpstream: sourceOldUpstream,
	})

	// For descendants, each will be rebased onto its parent (which is part of the moving stack)
	// Since these are topologically ordered, each parent will be rebased before its children
	sortedDescendants := eng.SortBranchesTopologically(descendants)
	for _, d := range sortedDescendants {
		if d.GetName() == source {
			continue // Already handled above
		}
		parent := d.GetParent()
		if parent == nil {
			continue
		}
		parentRev, revErr := eng.GetRevision(*parent)
		if revErr != nil {
			out.Debug("Failed to get revision for parent %s of %s: %v", parent.GetName(), d.GetName(), revErr)
		}

		// Get the old upstream (divergence point)
		dOldUpstream, divErr := eng.GetDivergencePoint(d.GetName())
		if divErr != nil {
			out.Debug("Failed to get divergence point for %s: %v", d.GetName(), divErr)
			dOldUpstream = parentRev // Fallback
		}

		rebaseSpecs = append(rebaseSpecs, engine.RebaseSpec{
			Branch:      d.GetName(),
			NewParent:   parentRev,
			OldUpstream: dOldUpstream,
		})
	}

	return rebaseSpecs
}
