// Package pluck provides functionality for extracting a single branch from a stack.
package pluck

import (
	"fmt"

	"stackit.dev/stackit/internal/actions"
	basehandler "stackit.dev/stackit/internal/actions/handler"
	"stackit.dev/stackit/internal/actions/validation"
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

	// Validate source branch
	if err := validation.ValidateSourceBranch(eng, source, "pluck"); err != nil {
		return err
	}

	// Validate target branch
	onto := opts.Onto
	if err := validation.ValidateTargetBranch(eng, source, onto, "pluck"); err != nil {
		return err
	}

	sourceBranch := eng.GetBranch(source)
	ontoBranch := eng.GetBranch(onto)

	// Get source's direct children (they will be reparented to grandparent)
	children := graph.ChildBranches(sourceBranch)

	// Cycle detection: ensure onto is not a descendant of source
	if graph.IsDescendant(sourceBranch, onto) {
		return fmt.Errorf("cannot pluck %s onto its own descendant %s", source, onto)
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
	sourceOldParentRev, _ := eng.GetDivergencePoint(source)

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
		// Get the old upstream (divergence point)
		childOldUpstream, divErr := eng.GetDivergencePoint(child.GetName())
		if divErr != nil {
			// Fallback to source revision if unavailable
			childOldUpstream = sourceRev
		}

		rebaseSpecs = append(rebaseSpecs, engine.RebaseSpec{
			Branch:      child.GetName(),
			NewParent:   grandparentRev,
			OldUpstream: childOldUpstream,
		})
	}

	// Source: rebase onto new parent
	rebaseSpecs = append(rebaseSpecs, engine.RebaseSpec{
		Branch:      source,
		NewParent:   ontoRev,
		OldUpstream: sourceOldParentRev,
	})

	// Validate rebases before modifying any state
	handler.OnStep(StepValidating, basehandler.StatusStarted, "Validating rebases...")
	validation, err := eng.ValidateRebases(gctx, rebaseSpecs)
	if err != nil {
		handler.OnStep(StepValidating, basehandler.StatusFailed, err.Error())
		return fmt.Errorf("failed to validate rebases: %w", err)
	}
	if !validation.Success {
		errorMsg := validation.ErrorMessage
		if len(validation.ConflictingFiles) > 0 {
			ctx.Logger.Debug("conflict detected during pluck validation",
				"branch", validation.FailedBranch,
				"files", validation.ConflictingFiles)
		}
		handler.OnStep(StepValidating, basehandler.StatusFailed, errorMsg)
		return fmt.Errorf("pluck would cause conflicts: %s on branch %s", errorMsg, validation.FailedBranch)
	}
	handler.OnStep(StepValidating, basehandler.StatusCompleted, "Validation passed")

	// Start the operation
	handler.Start(source, oldParentName, onto)

	// Step 1: Reparent children to grandparent
	var reparentedChildren []string
	if len(children) > 0 {
		handler.OnStep(StepReparentingChild, basehandler.StatusStarted, "Reparenting children...")

		for _, child := range children {
			if err := eng.SetParent(gctx, child, grandparentBranch); err != nil {
				handler.OnStep(StepReparentingChild, basehandler.StatusFailed, err.Error())
				return fmt.Errorf("failed to reparent %s to %s: %w", child.GetName(), grandparentBranch.GetName(), err)
			}
			handler.OnChildReparented(child.GetName(), source, grandparentBranch.GetName())
			reparentedChildren = append(reparentedChildren, child.GetName())
			out.Info("Reparented %s from %s to %s.",
				style.ColorBranchName(child.GetName(), false),
				style.ColorBranchName(source, false),
				style.ColorBranchName(grandparentBranch.GetName(), false))
		}

		handler.OnStep(StepReparentingChild, basehandler.StatusCompleted, "Children reparented")
	} else {
		handler.OnStep(StepReparentingChild, basehandler.StatusSkipped, "No children to reparent")
	}

	// Step 2: Move source to new parent, preserving the divergence point
	handler.OnStep(StepMovingSource, basehandler.StatusStarted, "Moving source branch...")
	if err := eng.SetParentPreservingDivergence(gctx, sourceBranch, ontoBranch, sourceOldParentRev); err != nil {
		handler.OnStep(StepMovingSource, basehandler.StatusFailed, err.Error())
		return fmt.Errorf("failed to set parent: %w", err)
	}

	// Update stack ID based on the pluck destination:
	// 1. Plucking to trunk creates a new independent stack with a fresh ID
	// 2. Plucking to a branch in a different stack inherits that stack's ID
	// 3. Plucking within the same stack keeps the existing ID (no update needed)
	oldStackID := eng.GetStackID(sourceBranch)
	newStackID := eng.GetStackID(ontoBranch)

	var targetStackID string
	switch {
	case eng.IsTrunk(ontoBranch):
		// Plucking to trunk creates a new stack
		targetStackID = eng.GenerateStackID(source)
		if err := eng.CreateStackRef(targetStackID, nil); err != nil {
			out.Warn("Failed to create stack ref: %v", err)
		}
	case newStackID != "" && newStackID != oldStackID:
		// Plucking to a different stack - inherit that stack's ID
		targetStackID = newStackID
	}

	// Update stack ID if needed (pluck only moves the source, not descendants)
	if targetStackID != "" {
		if err := eng.SetStackID(gctx, sourceBranch, targetStackID); err != nil {
			out.Warn("Failed to update stack ID for %s: %v", source, err)
		}
		out.Info("Stack membership updated for %s", source)
	}

	out.Info("Plucked %s from %s to %s.",
		style.ColorBranchName(source, true),
		style.ColorBranchName(oldParentName, false),
		style.ColorBranchName(onto, false))
	handler.OnStep(StepMovingSource, basehandler.StatusCompleted, "Source branch moved")

	// Step 3: Restack all affected branches
	handler.OnStep(StepRestackingOrphans, basehandler.StatusStarted, "Restacking branches...")

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
		handler.OnStep(StepRestackingOrphans, basehandler.StatusFailed, err.Error())
		return fmt.Errorf("failed to restack branches: %w", err)
	}
	handler.OnStep(StepRestackingOrphans, basehandler.StatusCompleted, "Branches restacked")

	handler.Complete(Result{
		SourceBranch:       source,
		OldParent:          oldParentName,
		NewParent:          onto,
		ReparentedChildren: reparentedChildren,
	})

	return nil
}
