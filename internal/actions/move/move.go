// Package move provides functionality for moving branches to different parents in the stack.
package move

import (
	"fmt"
	"strings"

	"stackit.dev/stackit/internal/actions"
	"stackit.dev/stackit/internal/app"
	"stackit.dev/stackit/internal/engine"
	"stackit.dev/stackit/internal/tui/style"
)

// Options contains options for the move command
type Options struct {
	Source      string // Branch to move (defaults to current branch)
	Onto        string // Branch to move onto
	SkipConfirm bool   // Skip confirmation prompt (--yes flag)
}

// Action performs the move operation
func Action(ctx *app.Context, opts Options, handler Handler) error {
	eng := ctx.Engine
	out := ctx.Output
	gctx := ctx.Context

	// Use null handler if none provided
	if handler == nil {
		handler = &NullHandler{}
	}
	defer handler.Cleanup()

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

	// Prevent moving trunk (check before tracking check since trunk might not be tracked)
	sourceBranch := eng.GetBranch(source)
	if sourceBranch.IsTrunk() {
		return fmt.Errorf("cannot move trunk branch")
	}

	// Validate source exists and is tracked
	if !sourceBranch.IsTracked() {
		return fmt.Errorf("branch %s is not tracked by Stackit", source)
	}

	// Prevent moving worktree anchor branches
	if sourceBranch.IsWorktreeAnchor() {
		return fmt.Errorf("cannot move worktree anchor branch %s", source)
	}

	// Validate onto is provided
	onto := opts.Onto
	if onto == "" {
		return fmt.Errorf("onto branch must be specified")
	}

	// Validate onto exists
	ontoBranch := eng.GetBranch(onto)
	if !ontoBranch.IsTrunk() && !ontoBranch.IsTracked() {
		// Check if it's an untracked branch
		allBranches := eng.AllBranches()
		found := false
		for _, branch := range allBranches {
			if branch.GetName() == onto {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("branch %s does not exist", onto)
		}
	}

	// Prevent moving onto worktree anchor branches
	if ontoBranch.IsWorktreeAnchor() {
		return fmt.Errorf("cannot move branch onto worktree anchor %s; use 'stackit create' in the worktree instead", onto)
	}

	// Prevent moving onto itself
	if source == onto {
		return fmt.Errorf("cannot move branch onto itself")
	}

	// Cycle detection: ensure onto is not a descendant of source
	sourceBranch = eng.GetBranch(source)
	descendants := graph.Range(sourceBranch, engine.StackRange{
		RecursiveChildren: true,
		IncludeCurrent:    true,
		RecursiveParents:  false,
	})
	for _, d := range descendants {
		if d.GetName() == onto {
			return fmt.Errorf("cannot move %s onto its own descendant %s", source, onto)
		}
	}

	// Get current parent for preview
	oldParent := sourceBranch.GetParent()
	oldParentName := ""
	if oldParent == nil {
		oldParentName = eng.Trunk().GetName()
	} else {
		oldParentName = oldParent.GetName()
	}

	// Prompt for confirmation in interactive mode (unless --yes flag is set)
	if handler.IsInteractive() && !opts.SkipConfirm {
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

		confirmed, err := handler.PromptConfirmMove(preview)
		if err != nil {
			return fmt.Errorf("failed to prompt for confirmation: %w", err)
		}
		if !confirmed {
			out.Info("Move canceled.")
			return nil
		}
	}

	// Check for scope change and potential rename
	var renamed bool
	sourceScope := sourceBranch.GetScope()
	ontoScope := ontoBranch.GetScope()
	if sourceScope.IsDefined() && ontoScope.IsDefined() && !sourceScope.Equal(ontoScope) {
		if handler.IsInteractive() && strings.Contains(source, sourceScope.String()) {
			confirmed, err := handler.PromptRename(source, sourceScope.String(), ontoScope.String())
			if err == nil && confirmed {
				newName := strings.Replace(source, sourceScope.String(), ontoScope.String(), 1)
				if err := eng.RenameBranch(gctx, eng.GetBranch(source), eng.GetBranch(newName)); err != nil {
					out.Info("Warning: failed to rename branch: %v", err)
				} else {
					handler.OnRename(source, newName)
					out.Info("Renamed branch %s to %s.", style.ColorBranchName(source, false), style.ColorBranchName(newName, true))
					source = newName
					sourceBranch = eng.GetBranch(source)
					renamed = true
				}
			}
		}
	}

	// Capture old divergence point to preserve it after reparenting
	// This ensures we only move the commits that belong to this branch.
	var oldParentRev string
	if meta, err := eng.Git().ReadMetadata(source); err == nil && meta.ParentBranchRevision != nil {
		oldParentRev = *meta.ParentBranchRevision
	}

	// Build rebase specs for validation
	// We need to validate that all rebases will succeed before modifying any state
	rebaseSpecs := make([]engine.RebaseSpec, 0, len(descendants))

	// Get the target parent's revision for the source branch rebase
	ontoRev, err := eng.GetRevision(ontoBranch)
	if err != nil {
		return fmt.Errorf("failed to get revision for %s: %w", onto, err)
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

		// Get the old upstream from metadata, falling back to parent revision if unavailable
		dOldUpstream := ""
		if meta, metaErr := eng.Git().ReadMetadata(d.GetName()); metaErr == nil && meta.ParentBranchRevision != nil {
			dOldUpstream = *meta.ParentBranchRevision
		}
		if dOldUpstream == "" {
			dOldUpstream = parentRev
		}

		rebaseSpecs = append(rebaseSpecs, engine.RebaseSpec{
			Branch:      d.GetName(),
			NewParent:   parentRev,
			OldUpstream: dOldUpstream,
		})
	}

	// Validate rebases in a temporary worktree before modifying any state
	handler.OnStep(StepValidating, StatusStarted, "Validating rebases...")
	validation, err := eng.ValidateRebases(gctx, rebaseSpecs)
	if err != nil {
		handler.OnStep(StepValidating, StatusFailed, err.Error())
		return fmt.Errorf("failed to validate rebases: %w", err)
	}
	if !validation.Success {
		handler.OnStep(StepValidating, StatusFailed, validation.ErrorMessage)
		return fmt.Errorf("move would cause conflicts: %s on branch %s", validation.ErrorMessage, validation.FailedBranch)
	}
	handler.OnStep(StepValidating, StatusCompleted, "Validation passed")

	// Start handler with branch info (oldParentName computed earlier for preview)
	handler.Start(source, oldParentName, onto)

	// Update parent in engine
	if err := eng.SetParent(gctx, sourceBranch, ontoBranch); err != nil {
		return fmt.Errorf("failed to set parent: %w", err)
	}

	// Rebuild graph after parent change for downstream traversals
	graph = engine.BuildStackGraph(eng, engine.SortStrategyAlphabetical, nil)

	// Restore old divergence point if it's still a valid ancestor
	if oldParentRev != "" {
		isAncestor, ancestorErr := eng.Git().IsAncestor(oldParentRev, source)
		if ancestorErr != nil {
			out.Debug("Failed to check if %s is ancestor of %s: %v", oldParentRev, source, ancestorErr)
		} else if isAncestor {
			if updateErr := eng.UpdateParentRevision(source, oldParentRev); updateErr != nil {
				out.Debug("Failed to update parent revision for %s: %v", source, updateErr)
			}
		}
	}

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

	newName := ""
	if renamed {
		newName = source
	}
	handler.Complete(Result{
		SourceBranch: source,
		OldParent:    oldParentName,
		NewParent:    onto,
		Renamed:      renamed,
		NewName:      newName,
	})
	return nil
}
