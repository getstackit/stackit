// Package pluck provides functionality for extracting a single branch from a stack.
package pluck

import (
	"fmt"

	"github.com/getstackit/stackit/internal/actions"
	basehandler "github.com/getstackit/stackit/internal/actions/handler"
	"github.com/getstackit/stackit/internal/actions/validation"
	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/output"
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

	graph := eng.Graph(engine.SortStrategyAlphabetical)

	// Default source to current branch
	source := opts.Source
	if source == "" {
		currentBranch := eng.CurrentBranch()
		if currentBranch == nil {
			return fmt.Errorf("not on a branch and no source branch specified")
		}
		source = currentBranch.GetName()
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

	// Cycle detection: ensure onto is not a descendant of source
	if graph.IsDescendant(sourceBranch, ontoBranch) {
		return fmt.Errorf("cannot pluck %s onto its own descendant %s", source, onto)
	}

	// Get source's direct children (they will be reparented to grandparent)
	children := graph.ChildBranches(sourceBranch)
	if err := actions.EnsureCanModifyHere(ctx, append(engine.BranchesOf(sourceBranch, ontoBranch), children...)...); err != nil {
		return err
	}

	// Take snapshot before modifying the repository, but after every check that
	// can reject the pluck, so a refusal leaves the undo stack untouched rather
	// than evicting a real recovery point with a no-op entry.
	snapshotOpts := actions.NewSnapshot("pluck",
		actions.WithFlagValue("--source", opts.Source),
		actions.WithFlagValue("--onto", opts.Onto),
	)
	actions.TakeBestEffortSnapshot(ctx, snapshotOpts)

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

		preview := Preview{
			SourceBranch:   source,
			OldParent:      oldParentName,
			NewParent:      onto,
			Children:       children.Names(),
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

	// Capture old divergence point for source branch, falling back to the
	// grandparent's revision (source's old parent) when unavailable.
	sourceOldParentRev, divErr := eng.GetDivergencePoint(source)
	if divErr != nil || sourceOldParentRev == "" {
		sourceOldParentRev = grandparentRev
	}

	// Children: rebase onto grandparent (source's old parent)
	childDivPoints := eng.BatchDivergencePoints(children)
	for _, child := range children {
		// Get the old upstream (divergence point)
		childOldUpstream, ok := childDivPoints.Rev(child.GetName())
		if !ok || childOldUpstream == "" {
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
			ctx.Logger.Debug("conflict detected during pluck validation branch=%v files=%v", validation.FailedBranch, validation.ConflictingFiles)
		}
		handler.OnStep(StepValidating, basehandler.StatusFailed, errorMsg)
		return fmt.Errorf("pluck would cause conflicts: %s on branch %s", errorMsg, validation.FailedBranch)
	}
	handler.OnStep(StepValidating, basehandler.StatusCompleted, "Validation passed")

	// Start the operation
	handler.Start(basehandler.Reparent{Branch: source, OldParent: oldParentName, NewParent: onto})

	// Steps 1 and 2 both change parents, and they are applied as a single
	// batch. Reparenting the children and then moving the source passes
	// through an intermediate shape where the grandparent holds both the
	// source and its former children — a fork the linear-stack validator
	// rejects even when the completed transformation is a valid chain.
	moves := make([]engine.BranchParentMove, 0, len(children)+1)
	for _, child := range children {
		moves = append(moves, engine.BranchParentMove{Branch: child.GetName(), NewParent: grandparentBranch.GetName()})
	}
	moves = append(moves, engine.BranchParentMove{Branch: source, NewParent: onto})

	if len(children) > 0 {
		handler.OnStep(StepReparentingChild, basehandler.StatusStarted, "Reparenting children...")
	} else {
		handler.OnStep(StepReparentingChild, basehandler.StatusSkipped, "No children to reparent")
	}
	handler.OnStep(StepMovingSource, basehandler.StatusStarted, "Moving source branch...")

	if err := eng.ReparentBranchesToParents(gctx, moves); err != nil {
		if len(children) > 0 {
			handler.OnStep(StepReparentingChild, basehandler.StatusFailed, err.Error())
		}
		handler.OnStep(StepMovingSource, basehandler.StatusFailed, err.Error())
		return fmt.Errorf("failed to set parent: %w", err)
	}

	// Step 1: children are now on the grandparent.
	var reparentedChildren []string
	if len(children) > 0 {
		for _, child := range children {
			handler.OnChildReparented(basehandler.Reparent{Branch: child.GetName(), OldParent: source, NewParent: grandparentBranch.GetName()})
			reparentedChildren = append(reparentedChildren, child.GetName())
			out.Info("Reparented %s from %s to %s.",
				output.BranchName(child.GetName()),
				output.BranchName(source),
				output.BranchName(grandparentBranch.GetName()))
		}
		handler.OnStep(StepReparentingChild, basehandler.StatusCompleted, "Children reparented")
	}

	// Step 2: the source is now on its new parent.
	if eng.IsTrunk(ontoBranch) {
		if _, err := eng.AssignBranchesToNewStack(gctx, sourceBranch, engine.BranchesOf(sourceBranch)); err != nil {
			out.Warn("Failed to update stack ID for %s: %v", source, err)
		}
	}

	out.Info("Plucked %s from %s to %s.",
		output.CurrentBranch(source),
		output.BranchName(oldParentName),
		output.BranchName(onto))
	handler.OnStep(StepMovingSource, basehandler.StatusCompleted, "Source branch moved")

	// Step 3: Restack all affected branches
	handler.OnStep(StepRestackingOrphans, basehandler.StatusStarted, "Restacking branches...")

	// Rebuild graph after parent changes
	graph = eng.Graph(engine.SortStrategyAlphabetical)

	// Collect all branches that need restacking:
	// 1. The children (now on grandparent) and their descendants
	// 2. The source branch (now on new parent)
	branchesToRestack := engine.Branches{}

	for _, child := range children {
		childBranch := eng.GetBranch(child.GetName())
		childDescendants := graph.Range(childBranch, engine.StackRange{
			RecursiveChildren: true,
			IncludeCurrent:    true,
			RecursiveParents:  false,
		})
		branchesToRestack = branchesToRestack.Concat(childDescendants)
	}

	// Add source branch
	sourceBranch = eng.GetBranch(source)
	branchesToRestack = branchesToRestack.Append(sourceBranch)

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
