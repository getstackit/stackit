// Package pluck provides functionality for extracting a single branch from a stack.
package pluck

import (
	"fmt"

	"stackit.dev/stackit/internal/actions"
	"stackit.dev/stackit/internal/app"
	"stackit.dev/stackit/internal/engine"
	"stackit.dev/stackit/internal/tui/style"
)

// Options contains options for the pluck command
type Options struct {
	Source      string // Branch to pluck (defaults to current branch)
	Onto        string // Branch to pluck onto
	SkipConfirm bool   // Skip confirmation prompt (--yes flag)
}

// Action performs the pluck operation.
// Pluck extracts a single branch from its current position and moves it to a new parent.
// Unlike move, pluck does NOT bring descendants along - they are reparented to the
// grandparent (the plucked branch's former parent).
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
	snapshotOpts := actions.NewSnapshot("pluck",
		actions.WithFlagValue("--source", opts.Source),
		actions.WithFlagValue("--onto", opts.Onto),
	)
	if err := eng.TakeSnapshot(snapshotOpts); err != nil {
		out.Debug("Failed to take snapshot: %v", err)
	}

	// Prevent plucking trunk
	sourceBranch := eng.GetBranch(source)
	if sourceBranch.IsTrunk() {
		return fmt.Errorf("cannot pluck trunk branch")
	}

	// Validate source exists and is tracked
	if !sourceBranch.IsTracked() {
		return fmt.Errorf("branch %s is not tracked by Stackit", source)
	}

	// Prevent plucking worktree anchor branches
	if sourceBranch.IsWorktreeAnchor() {
		return fmt.Errorf("cannot pluck worktree anchor branch %s", source)
	}

	// Validate onto is provided
	onto := opts.Onto
	if onto == "" {
		return fmt.Errorf("onto branch must be specified")
	}

	// Validate onto exists
	ontoBranch := eng.GetBranch(onto)
	if !ontoBranch.IsTrunk() && !ontoBranch.IsTracked() {
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

	// Prevent plucking onto worktree anchor branches
	if ontoBranch.IsWorktreeAnchor() {
		return fmt.Errorf("cannot pluck branch onto worktree anchor %s", onto)
	}

	// Prevent plucking onto itself
	if source == onto {
		return fmt.Errorf("cannot pluck branch onto itself")
	}

	// Get source's direct children (they will be reparented to grandparent)
	children := graph.ChildBranches(sourceBranch)

	// Get all descendants (for cycle detection)
	descendants := graph.Range(sourceBranch, engine.StackRange{
		RecursiveChildren: true,
		IncludeCurrent:    true,
		RecursiveParents:  false,
	})

	// Cycle detection: ensure onto is not a descendant of source
	for _, d := range descendants {
		if d.GetName() == onto {
			return fmt.Errorf("cannot pluck %s onto its own descendant %s", source, onto)
		}
	}

	// Get current parent (grandparent for children)
	oldParent := sourceBranch.GetParent()
	oldParentName := ""
	grandparentBranch := eng.Trunk()
	if oldParent == nil {
		oldParentName = eng.Trunk().GetName()
	} else {
		oldParentName = oldParent.GetName()
		grandparentBranch = *oldParent
	}

	// Prompt for confirmation in interactive mode
	if handler.IsInteractive() && !opts.SkipConfirm {
		commits, _ := eng.GetAllCommits(sourceBranch, engine.CommitFormatSubject)

		var childNames []string
		for _, c := range children {
			childNames = append(childNames, c.GetName())
		}

		preview := Preview{
			SourceBranch:   source,
			OldParent:      oldParentName,
			NewParent:      onto,
			Children:       childNames,
			ChildNewParent: oldParentName,
			Commits:        commits,
		}

		confirmed, err := handler.PromptConfirmPluck(preview)
		if err != nil {
			return fmt.Errorf("failed to prompt for confirmation: %w", err)
		}
		if !confirmed {
			out.Info("Pluck canceled.")
			return nil
		}
	}

	// Capture old divergence point for source branch
	var sourceOldParentRev string
	if meta, err := eng.Git().ReadMetadata(source); err == nil && meta.ParentBranchRevision != nil {
		sourceOldParentRev = *meta.ParentBranchRevision
	}

	// Build rebase specs for validation
	// Order matters: children first (they depend on grandparent), then source
	rebaseSpecs := make([]engine.RebaseSpec, 0, len(children)+1)

	// Get revisions needed for rebase specs
	ontoRev, err := eng.GetRevision(ontoBranch)
	if err != nil {
		return fmt.Errorf("failed to get revision for %s: %w", onto, err)
	}

	grandparentRev, err := eng.GetRevision(grandparentBranch)
	if err != nil {
		return fmt.Errorf("failed to get revision for %s: %w", grandparentBranch.GetName(), err)
	}

	sourceRev, err := eng.GetRevision(sourceBranch)
	if err != nil {
		return fmt.Errorf("failed to get revision for %s: %w", source, err)
	}

	// Children: rebase onto grandparent (source's old parent)
	for _, child := range children {
		// Get the old upstream from metadata, falling back to source revision if unavailable
		childOldUpstream := ""
		if meta, metaErr := eng.Git().ReadMetadata(child.GetName()); metaErr == nil && meta.ParentBranchRevision != nil {
			childOldUpstream = *meta.ParentBranchRevision
		}
		if childOldUpstream == "" {
			childOldUpstream = sourceRev
		}

		rebaseSpecs = append(rebaseSpecs, engine.RebaseSpec{
			Branch:      child.GetName(),
			NewParent:   grandparentRev,
			OldUpstream: childOldUpstream,
		})
	}

	// Source: rebase onto new parent
	sourceOldUpstream := sourceOldParentRev
	if sourceOldUpstream == "" {
		sourceOldUpstream = grandparentRev
	}
	rebaseSpecs = append(rebaseSpecs, engine.RebaseSpec{
		Branch:      source,
		NewParent:   ontoRev,
		OldUpstream: sourceOldUpstream,
	})

	// Validate rebases before modifying any state
	handler.OnStep(StepValidating, StatusStarted, "Validating rebases...")
	validation, err := eng.ValidateRebases(gctx, rebaseSpecs)
	if err != nil {
		handler.OnStep(StepValidating, StatusFailed, err.Error())
		return fmt.Errorf("failed to validate rebases: %w", err)
	}
	if !validation.Success {
		handler.OnStep(StepValidating, StatusFailed, validation.ErrorMessage)
		return fmt.Errorf("pluck would cause conflicts: %s on branch %s", validation.ErrorMessage, validation.FailedBranch)
	}
	handler.OnStep(StepValidating, StatusCompleted, "Validation passed")

	// Start the operation
	handler.Start(source, oldParentName, onto)

	// Step 1: Reparent children to grandparent
	var reparentedChildren []string
	if len(children) > 0 {
		handler.OnStep(StepReparentingChild, StatusStarted, "Reparenting children...")

		for _, child := range children {
			if err := eng.SetParent(gctx, child, grandparentBranch); err != nil {
				handler.OnStep(StepReparentingChild, StatusFailed, err.Error())
				return fmt.Errorf("failed to reparent %s to %s: %w", child.GetName(), grandparentBranch.GetName(), err)
			}
			handler.OnChildReparented(child.GetName(), source, grandparentBranch.GetName())
			reparentedChildren = append(reparentedChildren, child.GetName())
			out.Info("Reparented %s from %s to %s.",
				style.ColorBranchName(child.GetName(), false),
				style.ColorBranchName(source, false),
				style.ColorBranchName(grandparentBranch.GetName(), false))
		}

		handler.OnStep(StepReparentingChild, StatusCompleted, "Children reparented")
	} else {
		handler.OnStep(StepReparentingChild, StatusSkipped, "No children to reparent")
	}

	// Step 2: Move source to new parent
	handler.OnStep(StepMovingSource, StatusStarted, "Moving source branch...")
	if err := eng.SetParent(gctx, sourceBranch, ontoBranch); err != nil {
		handler.OnStep(StepMovingSource, StatusFailed, err.Error())
		return fmt.Errorf("failed to set parent: %w", err)
	}

	// Restore old divergence point if valid
	if sourceOldParentRev != "" {
		isAncestor, ancestorErr := eng.Git().IsAncestor(sourceOldParentRev, source)
		if ancestorErr != nil {
			out.Debug("Failed to check if %s is ancestor of %s: %v", sourceOldParentRev, source, ancestorErr)
		} else if isAncestor {
			if updateErr := eng.UpdateParentRevision(source, sourceOldParentRev); updateErr != nil {
				out.Debug("Failed to update parent revision for %s: %v", source, updateErr)
			}
		}
	}

	out.Info("Plucked %s from %s to %s.",
		style.ColorBranchName(source, true),
		style.ColorBranchName(oldParentName, false),
		style.ColorBranchName(onto, false))
	handler.OnStep(StepMovingSource, StatusCompleted, "Source branch moved")

	// Step 3: Restack all affected branches
	handler.OnStep(StepRestackingOrphans, StatusStarted, "Restacking branches...")

	// Rebuild graph after parent changes
	graph = engine.BuildStackGraph(eng, engine.SortStrategyAlphabetical, nil)

	// Collect all branches that need restacking:
	// 1. The children (now on grandparent) and their descendants
	// 2. The source branch (now on new parent)
	var branchesToRestack []engine.Branch

	for _, child := range children {
		childBranch := eng.GetBranch(child.GetName())
		childDescendants := graph.Range(childBranch, engine.StackRange{
			RecursiveChildren: true,
			IncludeCurrent:    true,
			RecursiveParents:  false,
		})
		branchesToRestack = append(branchesToRestack, childDescendants...)
	}

	// Add source branch
	sourceBranch = eng.GetBranch(source)
	branchesToRestack = append(branchesToRestack, sourceBranch)

	if err := actions.RestackBranches(ctx, branchesToRestack); err != nil {
		handler.OnStep(StepRestackingOrphans, StatusFailed, err.Error())
		return fmt.Errorf("failed to restack branches: %w", err)
	}
	handler.OnStep(StepRestackingOrphans, StatusCompleted, "Branches restacked")

	handler.Complete(Result{
		SourceBranch:       source,
		OldParent:          oldParentName,
		NewParent:          onto,
		ReparentedChildren: reparentedChildren,
	})

	return nil
}
