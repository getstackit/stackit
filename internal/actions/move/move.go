// Package move provides functionality for moving branches to different parents in the stack.
package move

import (
	"fmt"
	"strings"

	"github.com/getstackit/stackit/internal/actions"
	"github.com/getstackit/stackit/internal/actions/handler"
	"github.com/getstackit/stackit/internal/actions/validation"
	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/output"
	"github.com/getstackit/stackit/internal/tui/style"
)

// Options contains options for the move command
type Options struct {
	Source      string // Branch to move (defaults to current branch)
	Onto        string // Branch to move onto
	SkipConfirm bool   // Skip confirmation prompt (--yes flag)
	DryRun      bool   // If true, only shows what would happen without making changes
	AutoRename  bool   // Auto-rename branch when scope changes (non-interactive mode)
	// RebaseSpecs optionally provides precomputed specs (e.g. from interactive selection).
	RebaseSpecs []engine.RebaseSpec
}

type movePlan struct {
	source        string
	onto          string
	sourceBranch  engine.Branch
	ontoBranch    engine.Branch
	descendants   engine.Branches
	oldParentName string
	oldParentRev  string
	rebaseSpecs   []engine.RebaseSpec
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

	source, err := resolveSource(eng, opts.Source)
	if err != nil {
		return err
	}

	if err := ensureOnto(ctx, &opts, h, source); err != nil {
		return err
	}

	takeSnapshot(ctx, opts)

	if err := validateMoveTargets(eng, source, opts.Onto); err != nil {
		return err
	}

	plan, err := buildMovePlan(eng, out, source, opts.Onto, opts.RebaseSpecs)
	if err != nil {
		return err
	}

	if opts.DryRun {
		return dryRun(ctx, plan.source, plan.oldParentName, plan.onto, plan.sourceBranch, plan.descendants, plan.rebaseSpecs)
	}

	proceed, err := validateForExecution(ctx, h, plan, opts.SkipConfirm)
	if err != nil {
		return err
	}
	if !proceed {
		return nil
	}

	renamed, source, sourceBranch := maybeRename(ctx, h, opts, plan)
	plan.source = source
	plan.sourceBranch = sourceBranch

	h.Start(plan.source, plan.oldParentName, plan.onto)

	if err := eng.ReparentBranch(gctx, plan.sourceBranch, plan.ontoBranch); err != nil {
		return fmt.Errorf("failed to set parent: %w", err)
	}

	updateStackIDsAfterMove(ctx, plan, plan.source, plan.sourceBranch)

	if err := restackAndMark(ctx, plan, plan.sourceBranch); err != nil {
		return err
	}

	completeMove(h, plan, renamed)
	return nil
}

func resolveSource(eng engine.Engine, source string) (string, error) {
	if source == "" {
		currentBranch := eng.CurrentBranch()
		if currentBranch == nil {
			return "", fmt.Errorf("not on a branch and no source branch specified")
		}
		return currentBranch.GetName(), nil
	}
	return source, nil
}

func ensureOnto(ctx *app.Context, opts *Options, h Handler, source string) error {
	if opts.Onto != "" {
		return nil
	}
	if opts.DryRun {
		return fmt.Errorf("--onto flag is required when using --dry-run")
	}
	if !h.IsInteractive() {
		return validation.ValidateTargetBranch(ctx.Engine, source, "", "move")
	}
	selected, rebaseSpecs, err := h.PromptSelectOnto(ctx, source)
	if err != nil {
		return err
	}
	opts.Onto = selected
	opts.RebaseSpecs = rebaseSpecs
	return nil
}

func takeSnapshot(ctx *app.Context, opts Options) {
	snapshotOpts := actions.NewSnapshot("move",
		actions.WithFlagValue("--source", opts.Source),
		actions.WithFlagValue("--onto", opts.Onto),
	)
	actions.TakeBestEffortSnapshot(ctx, snapshotOpts)
}

func validateMoveTargets(eng engine.Engine, source, onto string) error {
	if err := validation.ValidateSourceBranch(eng, source, "move"); err != nil {
		return err
	}
	if err := validation.ValidateTargetBranch(eng, source, onto, "move"); err != nil {
		return err
	}
	return nil
}

func buildMovePlan(eng engine.Engine, out output.Output, source, onto string, rebaseSpecs []engine.RebaseSpec) (*movePlan, error) {
	graph := eng.Graph(engine.SortStrategyAlphabetical)

	sourceBranch := eng.GetBranch(source)
	ontoBranch := eng.GetBranch(onto)

	if graph.IsDescendant(sourceBranch, ontoBranch) {
		return nil, fmt.Errorf("cannot move %s onto its own descendant %s", source, onto)
	}

	descendants := graph.Range(sourceBranch, engine.StackRange{
		RecursiveChildren: true,
		IncludeCurrent:    true,
		RecursiveParents:  false,
	})

	oldParent := sourceBranch.GetParent()
	oldParentName := sourceBranch.GetParentOrTrunk()
	oldParentRev, _ := eng.GetDivergencePoint(source)

	if len(rebaseSpecs) == 0 {
		rebaseSpecs = BuildRebaseSpecs(eng, out, source, onto, oldParent, oldParentRev, descendants)
	}

	return &movePlan{
		source:        source,
		onto:          onto,
		sourceBranch:  sourceBranch,
		ontoBranch:    ontoBranch,
		descendants:   descendants,
		oldParentName: oldParentName,
		oldParentRev:  oldParentRev,
		rebaseSpecs:   rebaseSpecs,
	}, nil
}

// validateForExecution validates the planned move (in interactive mode it also
// confirms with the user). It returns (proceed, error): proceed=false with a
// nil error means the user canceled and the caller should stop without
// surfacing an error.
func validateForExecution(ctx *app.Context, h Handler, plan *movePlan, skipConfirm bool) (bool, error) {
	if h.IsInteractive() && !skipConfirm {
		return confirmInteractive(ctx, h, plan)
	}
	return validateNonInteractive(ctx, h, plan)
}

func confirmInteractive(ctx *app.Context, h Handler, plan *movePlan) (bool, error) {
	eng := ctx.Engine
	out := ctx.Output

	validation, validationErr := eng.ValidateRebases(ctx.Context, plan.rebaseSpecs)
	commits, _ := eng.GetAllCommits(plan.sourceBranch, engine.CommitFormatSubject)

	preview := buildPreview(plan, commits, validation, validationErr)

	confirmed, err := h.PromptConfirmMove(preview)
	if err != nil {
		return false, fmt.Errorf("failed to prompt for confirmation: %w", err)
	}
	if !confirmed {
		out.Info("Move canceled.")
		return false, nil
	}

	// System errors (worktree setup, etc.) can't be resolved by the user,
	// so abort instead of dropping into the conflict workflow.
	if validationErr != nil {
		return false, fmt.Errorf("failed to validate rebases: %w", validationErr)
	}

	// Conflicts are fine to proceed with — the restack call below will enter
	// the conflict workflow so the user can resolve and `stackit continue`.
	return true, nil
}

func validateNonInteractive(ctx *app.Context, h Handler, plan *movePlan) (bool, error) {
	h.OnStep(StepValidating, handler.StatusStarted, "Validating rebases...")
	validation, err := ctx.Engine.ValidateRebases(ctx.Context, plan.rebaseSpecs)
	if err != nil {
		h.OnStep(StepValidating, handler.StatusFailed, err.Error())
		return false, fmt.Errorf("failed to validate rebases: %w", err)
	}
	if !validation.Success {
		h.OnStep(StepValidating, handler.StatusFailed, validation.ErrorMessage)
		return false, fmt.Errorf("move would cause conflicts: %s on branch %s", validation.ErrorMessage, validation.FailedBranch)
	}
	h.OnStep(StepValidating, handler.StatusCompleted, "Validation passed")
	return true, nil
}

func buildPreview(plan *movePlan, commits []string, validation *engine.RebaseValidation, validationErr error) Preview {
	preview := Preview{
		SourceBranch: plan.source,
		OldParent:    plan.oldParentName,
		NewParent:    plan.onto,
		Commits:      commits,
		Descendants:  descendantNames(plan.descendants, plan.source),
	}

	if validationErr == nil && validation != nil && !validation.Success {
		preview.HasConflicts = true
		preview.ConflictBranch = validation.FailedBranch
		preview.ConflictError = validation.ErrorMessage
		preview.ConflictingFiles = validation.ConflictingFiles
	}

	return preview
}

func descendantNames(descendants engine.Branches, source string) []string {
	return descendants.Filter(func(d engine.Branch) bool {
		return d.GetName() != source
	}).Names()
}

func maybeRename(ctx *app.Context, h Handler, opts Options, plan *movePlan) (bool, string, engine.Branch) {
	eng := ctx.Engine
	out := ctx.Output

	source := plan.source
	sourceBranch := plan.sourceBranch
	ontoBranch := plan.ontoBranch

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
			if err := eng.RenameBranch(ctx.Context, eng.GetBranch(source), eng.GetBranch(newName)); err != nil {
				out.Info("Warning: failed to rename branch: %v", err)
			} else {
				h.OnRename(source, newName)
				out.Info("Renamed branch %s to %s.", style.ColorBranchName(source, false), style.ColorBranchName(newName, true))
				source = newName
				sourceBranch = eng.GetBranch(source)
				return true, source, sourceBranch
			}
		}
	}

	return false, source, sourceBranch
}

func updateStackIDsAfterMove(ctx *app.Context, plan *movePlan, source string, sourceBranch engine.Branch) {
	eng := ctx.Engine
	out := ctx.Output

	if plan.oldParentName == plan.onto {
		return
	}

	oldStackID := eng.GetStackID(sourceBranch)
	if oldStackID == "" {
		return
	}

	newStackID := eng.GetStackID(plan.ontoBranch)

	var targetStackID string
	switch {
	case eng.IsTrunk(plan.ontoBranch):
		targetStackID = eng.GenerateStackID(source)
		if err := eng.CreateStackRef(targetStackID, nil); err != nil {
			out.Warn("Failed to create stack ref: %v", err)
		}
	case newStackID != "" && newStackID != oldStackID:
		targetStackID = newStackID
	}

	if targetStackID == "" {
		return
	}

	for _, d := range plan.descendants {
		if err := eng.SetStackID(ctx.Context, d, targetStackID); err != nil {
			out.Warn("Failed to update stack ID for %s: %v", d.GetName(), err)
		}
	}
	out.Info("Stack membership updated for %s", source)
}

func restackAndMark(ctx *app.Context, plan *movePlan, sourceBranch engine.Branch) error {
	eng := ctx.Engine
	out := ctx.Output

	graph := eng.Graph(engine.SortStrategyAlphabetical)

	out.Info("Moved %s from %s to %s.",
		style.ColorBranchName(plan.source, true),
		style.ColorBranchName(plan.oldParentName, false),
		style.ColorBranchName(plan.onto, false))

	branchesToRestack := graph.Range(sourceBranch, engine.StackRange{
		RecursiveChildren: true,
		IncludeCurrent:    true,
		RecursiveParents:  false,
	})

	if err := actions.RestackBranches(ctx, branchesToRestack); err != nil {
		return fmt.Errorf("failed to restack branches: %w", err)
	}

	affectedBranches := []string{plan.source}
	if plan.oldParentName != eng.Trunk().GetName() {
		affectedBranches = append(affectedBranches, plan.oldParentName)
	}
	if err := eng.BatchMarkNeedsPRBodyUpdate(ctx, affectedBranches); err != nil {
		out.Debug("Failed to mark branches for PR body update: %v", err)
	}

	return nil
}

func completeMove(h Handler, plan *movePlan, renamed bool) {
	newName := ""
	if renamed {
		newName = plan.source
	}
	h.Complete(Result{
		SourceBranch: plan.source,
		OldParent:    plan.oldParentName,
		NewParent:    plan.onto,
		Renamed:      renamed,
		NewName:      newName,
	})
}

// dryRun validates and prints what the move would do without making changes.
func dryRun(ctx *app.Context, source, oldParentName, onto string, sourceBranch engine.Branch, descendants engine.Branches, rebaseSpecs []engine.RebaseSpec) error {
	eng := ctx.Engine
	out := ctx.Output
	gctx := ctx.Context

	// Run validation
	validation, validationErr := eng.ValidateRebases(gctx, rebaseSpecs)

	// Get commits that will be moved
	commits, _ := eng.GetAllCommits(sourceBranch, engine.CommitFormatSubject)

	// Get descendant names (excluding source itself)
	descNames := descendantNames(descendants, source)

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
	if len(descNames) > 0 {
		out.Info("\nDescendant branches to restack (%d):", len(descNames))
		for _, name := range descNames {
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
func BuildRebaseSpecs(eng engine.Engine, out output.Output, source, onto string, oldParent *engine.Branch, oldParentRev string, descendants engine.Branches) []engine.RebaseSpec {
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
