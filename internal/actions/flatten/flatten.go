// Package flatten provides functionality for flattening stacked branches closer to trunk.
package flatten

import (
	"fmt"

	"github.com/getstackit/stackit/internal/actions"
	basehandler "github.com/getstackit/stackit/internal/actions/handler"
	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/output"
)

// Options contains options for the flatten command
type Options struct {
	BranchName  string // Branch to start flattening from (defaults to current branch)
	SkipConfirm bool   // Skip confirmation prompt (--yes flag)
}

// HasFlattenWork reports whether Action has at least one feature branch in
// the requested stack that's a candidate for flattening. Use this from the
// CLI to gate TUI initialization — if there's no candidate, starting the
// bubbletea runner only flashes startup/teardown escape codes and silences
// the explanatory "no branches to flatten" message via quiet mode.
//
// This does *not* run the expensive flatten plan; a candidate found here may
// still end up unmoved after validation, in which case Action falls back to
// the handler.Complete summary path.
func HasFlattenWork(ctx *app.Context, opts Options) (bool, error) {
	eng := ctx.Engine
	branchName, err := actions.ResolveBranchName(eng, opts.BranchName)
	if err != nil {
		return false, err
	}
	branch := eng.GetBranch(branchName)
	if !branch.IsTracked() && !branch.IsTrunk() {
		return false, fmt.Errorf("branch %q is not tracked by stackit", branchName)
	}

	graph := eng.Graph(engine.SortStrategyAlphabetical)
	branches := graph.FullStack(branch)
	trunk := eng.Trunk()
	for _, b := range branches {
		if !b.IsTrunk() && b.GetName() != trunk.GetName() {
			return true, nil
		}
	}
	return false, nil
}

// Action performs the flatten operation.
// Flatten analyzes the stack and moves branches as close to trunk as possible
// while respecting dependencies (branches that would conflict stay in place).
func Action(ctx *app.Context, opts Options, handler Handler) error {
	eng := ctx.Engine
	out := ctx.Output
	gctx := ctx.Context

	// Use null handler if none provided
	if handler == nil {
		handler = &NullHandler{}
	}
	defer handler.Cleanup()

	// Resolve branch name
	branchName, err := actions.ResolveBranchName(eng, opts.BranchName)
	if err != nil {
		return err
	}

	// Validate branch exists and is tracked
	branch := eng.GetBranch(branchName)
	if !branch.IsTracked() && !branch.IsTrunk() {
		return fmt.Errorf("branch %q is not tracked by stackit", branchName)
	}

	// Build stack graph to get all related branches
	graph := eng.Graph(engine.SortStrategyAlphabetical)
	branches := graph.FullStack(branch)

	if len(branches) == 0 {
		out.Info("No branches to flatten.")
		return nil
	}

	// Sort topologically (parents before children)
	sortedBranches := eng.SortBranchesTopologically(branches)

	// Filter out trunk - we only flatten feature branches
	trunk := eng.Trunk()
	featureBranches := engine.Branches{}
	for _, b := range sortedBranches {
		if !b.IsTrunk() && b.GetName() != trunk.GetName() {
			featureBranches = featureBranches.Append(b)
		}
	}

	if len(featureBranches) == 0 {
		out.Info("No feature branches to flatten.")
		return nil
	}

	handler.Start(len(featureBranches))
	handler.OnStep(StepAnalyzing, basehandler.StatusStarted, "Analyzing stack structure...")

	// Take snapshot before any modifications
	snapshotOpts := actions.NewSnapshot("flatten",
		actions.WithArg(branchName),
	)
	actions.TakeBestEffortSnapshot(ctx, snapshotOpts)

	// Build the initial flatten plan by testing which branches can move closer to trunk
	plan, err := buildFlattenPlan(ctx, eng, featureBranches, trunk, func(current, total int, branchName string) {
		handler.OnValidationProgress(current, total, branchName)
	})
	if err != nil {
		handler.OnStep(StepAnalyzing, basehandler.StatusFailed, err.Error())
		return fmt.Errorf("failed to build flatten plan: %w", err)
	}

	handler.OnStep(StepAnalyzing, basehandler.StatusCompleted, fmt.Sprintf("Found %d potential branches to move", len(plan.Moves)))

	// If nothing to move, we're done
	if len(plan.Moves) == 0 {
		out.Info("All branches are already as close to trunk as possible.")
		handler.Complete(Result{
			MovedCount:     0,
			UnchangedCount: len(featureBranches),
		})
		return nil
	}

	// Validate all branches that will be restacked (moved branches + their descendants)
	handler.OnStep(StepValidating, basehandler.StatusStarted, "Validating moves and descendants...")

	// Collect all branches that will be affected by each move (including descendants)
	allBranchesToValidate := collectAllBranchesToRestack(eng, graph, plan.Moves)

	// Sort branches topologically (parents before children) for correct validation order
	branchObjects := engine.Branches{}
	for _, name := range allBranchesToValidate {
		branchObjects = branchObjects.Append(eng.GetBranch(name))
	}
	sortedForValidation := eng.SortBranchesTopologically(branchObjects)

	// Build rebase specs for all branches in topological order
	allRebaseSpecs := buildRebaseSpecsForAll(eng, plan, sortedForValidation)

	// Report progress for validation
	handler.OnValidationProgress(0, len(allRebaseSpecs), "validating...")

	// Validate ALL specs together in a single call - this is crucial!
	// ValidateRebases tracks rebased SHAs, so chained rebases work correctly.
	// Parents are rebased first, and their new SHAs are used for child rebases.
	conflicts := make(map[string]string) // branch -> error message
	validation, err := eng.ValidateRebases(gctx, allRebaseSpecs)
	if err != nil {
		handler.OnStep(StepValidating, basehandler.StatusFailed, err.Error())
		return fmt.Errorf("failed to validate rebases: %w", err)
	}

	handler.OnValidationProgress(len(allRebaseSpecs), len(allRebaseSpecs), "done")

	if !validation.Success {
		conflicts[validation.FailedBranch] = validation.ErrorMessage
		if len(validation.ConflictingFiles) > 0 {
			ctx.Logger.Debug("conflict detected during flatten validation branch=%v files=%v", validation.FailedBranch, validation.ConflictingFiles)
		}
	}

	// Filter moves to exclude those that would cause conflicts (directly or via descendants)
	filteredPlan, excludedBranches := filterPlanExcludingConflicts(plan, conflicts, graph, eng)

	switch {
	case len(conflicts) > 0 && len(filteredPlan.Moves) > 0:
		handler.OnStep(StepValidating, basehandler.StatusCompleted,
			fmt.Sprintf("Validated: %d moves safe, %d excluded due to conflicts",
				len(filteredPlan.Moves), len(excludedBranches)))
	case len(conflicts) > 0:
		handler.OnStep(StepValidating, basehandler.StatusCompleted, "All moves would cause conflicts")
	default:
		handler.OnStep(StepValidating, basehandler.StatusCompleted, "All moves validated successfully")
	}

	// Build preview
	preview := Preview{
		Moves:            filteredPlan.Moves,
		UnchangedCount:   filteredPlan.UnchangedCount,
		ExcludedBranches: excludedBranches,
	}

	// Prompt for confirmation (if interactive and not skipping)
	if handler.IsInteractive() && !opts.SkipConfirm {
		confirmed, promptErr := handler.PromptConfirmFlatten(preview)
		if promptErr != nil {
			return fmt.Errorf("failed to prompt for confirmation: %w", promptErr)
		}
		if !confirmed {
			out.Info("Flatten canceled.")
			return nil
		}
	}

	// If no moves are safe, exit
	if len(filteredPlan.Moves) == 0 {
		out.Info("No branches can be safely flattened.")
		handler.Complete(Result{
			MovedCount:     0,
			UnchangedCount: filteredPlan.UnchangedCount,
		})
		return nil
	}

	// Execute the flatten
	handler.OnStep(StepFlattening, basehandler.StatusStarted, "Moving branches...")

	// Update parent pointers for all planned moves in one batch. Each branch
	// moves to a different parent, so use the per-branch reparent batch; it
	// captures every divergence point before mutating.
	moves := make([]engine.BranchParentMove, len(filteredPlan.Moves))
	for i, move := range filteredPlan.Moves {
		moves[i] = engine.BranchParentMove{Branch: move.Branch, NewParent: move.NewParent}
	}
	if err := eng.ReparentBranchesToParents(gctx, moves); err != nil {
		handler.OnStep(StepFlattening, basehandler.StatusFailed, err.Error())
		return fmt.Errorf("failed to update flattened parent relationships: %w", err)
	}
	for _, move := range filteredPlan.Moves {
		handler.OnBranchMoved(move)
		out.Info("  %s: %s -> %s",
			output.BranchName(move.Branch),
			output.Dim(move.OldParent),
			output.BranchName(move.NewParent))
	}

	handler.OnStep(StepFlattening, basehandler.StatusCompleted, "Parent pointers updated")

	// Restack all affected branches
	handler.OnStep(StepRestacking, basehandler.StatusStarted, "Restacking branches...")

	// Rebuild graph after parent changes
	graph = eng.Graph(engine.SortStrategyAlphabetical)

	// Collect all branches that need restacking (moved branches and their descendants)
	branchesToRestack := engine.Branches{}
	seenRestackBranches := make(map[string]bool)
	for _, move := range filteredPlan.Moves {
		moveBranch := eng.GetBranch(move.Branch)
		descendants := graph.Range(moveBranch, engine.StackRange{
			RecursiveChildren: true,
			IncludeCurrent:    true,
			RecursiveParents:  false,
		})
		for _, branch := range descendants {
			name := branch.GetName()
			if seenRestackBranches[name] {
				continue
			}
			seenRestackBranches[name] = true
			branchesToRestack = branchesToRestack.Append(branch)
		}
	}
	branchesToRestack = eng.SortBranchesTopologically(branchesToRestack)

	if err := actions.RestackBranches(ctx, branchesToRestack); err != nil {
		handler.OnStep(StepRestacking, basehandler.StatusFailed, err.Error())
		return fmt.Errorf("failed to restack branches: %w", err)
	}

	handler.OnStep(StepRestacking, basehandler.StatusCompleted, "Branches restacked")

	out.Info("\nFlatten complete: %d branches moved, %d unchanged.", len(filteredPlan.Moves), filteredPlan.UnchangedCount)

	handler.Complete(Result{
		MovedCount:     len(filteredPlan.Moves),
		UnchangedCount: filteredPlan.UnchangedCount,
	})

	return nil
}

// collectAllBranchesToRestack collects all branches that will be affected by the moves,
// including the moved branches and all their descendants.
func collectAllBranchesToRestack(eng engine.Engine, graph *engine.StackGraph, moves []basehandler.Reparent) []string {
	seen := make(map[string]bool)
	var result []string

	for _, move := range moves {
		moveBranch := eng.GetBranch(move.Branch)
		descendants := graph.Range(moveBranch, engine.StackRange{
			RecursiveChildren: true,
			IncludeCurrent:    true,
			RecursiveParents:  false,
		})

		for _, b := range descendants {
			name := b.GetName()
			if !seen[name] {
				seen[name] = true
				result = append(result, name)
			}
		}
	}

	return result
}

// buildRebaseSpecsForAll builds rebase specs for all branches that will be affected,
// accounting for cascading parent changes when ancestors are moved.
func buildRebaseSpecsForAll(eng engine.Engine, plan *flattenPlan, branches engine.Branches) []engine.RebaseSpec {
	// Build a map of existing rebase specs from the plan
	existingSpecs := make(map[string]engine.RebaseSpec)
	for _, spec := range plan.RebaseSpecs {
		existingSpecs[spec.Branch] = spec
	}

	// Resolve divergence points and parent revisions for every branch up front
	// in batched reads, instead of one git process pair per branch below.
	divergence := eng.BatchDivergencePoints(branches)
	parentNames := make([]string, 0, len(branches))
	for _, b := range branches {
		if parent := b.GetParent(); parent != nil {
			parentNames = append(parentNames, parent.GetName())
		}
	}
	parentRevisions, _ := eng.GetRevisions(parentNames)

	specs := make([]engine.RebaseSpec, 0, len(branches))

	for _, branch := range branches {
		branchName := branch.GetName()

		// If we already have a spec from the plan, use it
		if spec, ok := existingSpecs[branchName]; ok {
			specs = append(specs, spec)
			continue
		}

		// This is a descendant branch - build its spec based on its parent's new position
		parent := branch.GetParent()
		if parent == nil {
			continue
		}

		// Get the old upstream (divergence point)
		oldUpstream, ok := divergence[branchName]
		if !ok {
			continue
		}

		// The new parent revision is the tip of the parent branch
		// (which will be its rebased position after the flatten)
		newParentRev, ok := parentRevisions[parent.GetName()]
		if !ok || newParentRev == "" {
			continue
		}

		specs = append(specs, engine.RebaseSpec{
			Branch:      branchName,
			NewParent:   newParentRev,
			OldUpstream: oldUpstream,
		})
	}

	return specs
}

// filterPlanExcludingConflicts filters out moves where the moved branch or any of its
// descendants have code dependencies. Returns the filtered plan and a list of branches kept in place.
func filterPlanExcludingConflicts(plan *flattenPlan, conflicts map[string]string, graph *engine.StackGraph, eng engine.Engine) (*flattenPlan, []ExcludedBranch) {
	if len(conflicts) == 0 {
		return plan, nil
	}

	// Build a set of moves to exclude (branches whose moves would cause conflicts)
	movesToExclude := make(map[string]string) // branch -> reason

	// For each move, check if the branch or any of its descendants would conflict
	for _, move := range plan.Moves {
		moveBranch := eng.GetBranch(move.Branch)
		descendants := graph.Range(moveBranch, engine.StackRange{
			RecursiveChildren: true,
			IncludeCurrent:    true,
			RecursiveParents:  false,
		})

		for _, desc := range descendants {
			descName := desc.GetName()
			if errMsg, hasConflict := conflicts[descName]; hasConflict {
				if descName == move.Branch {
					movesToExclude[move.Branch] = "needs resolution: " + errMsg
				} else {
					movesToExclude[move.Branch] = fmt.Sprintf("%s depends on this branch", descName)
				}
				break
			}
		}
	}

	// Build filtered plan
	filtered := &flattenPlan{
		Moves:          make([]basehandler.Reparent, 0),
		RebaseSpecs:    make([]engine.RebaseSpec, 0),
		UnchangedCount: plan.UnchangedCount,
	}

	var excluded []ExcludedBranch

	// Create a map of branch -> rebase spec for quick lookup
	specMap := make(map[string]engine.RebaseSpec)
	for _, spec := range plan.RebaseSpecs {
		specMap[spec.Branch] = spec
	}

	for _, move := range plan.Moves {
		if reason, shouldExclude := movesToExclude[move.Branch]; shouldExclude {
			excluded = append(excluded, ExcludedBranch{
				Branch: move.Branch,
				Reason: reason,
			})
			filtered.UnchangedCount++
		} else {
			filtered.Moves = append(filtered.Moves, move)
			if spec, ok := specMap[move.Branch]; ok {
				filtered.RebaseSpecs = append(filtered.RebaseSpecs, spec)
			}
		}
	}

	return filtered, excluded
}

// flattenPlan contains the calculated flatten operations
type flattenPlan struct {
	Moves          []basehandler.Reparent // Branches that will be moved
	UnchangedCount int                    // Number of branches that won't change
	RebaseSpecs    []engine.RebaseSpec    // Specs for validating all moves
}

// AnalysisProgressFunc is called to report progress during flatten plan analysis.
type AnalysisProgressFunc func(current, total int, branchName string)

// buildFlattenPlan calculates which branches can be moved closer to trunk.
// For each branch (in topological order), it tests if the branch can rebase
// onto trunk or any intermediate branch that's closer to trunk.
func buildFlattenPlan(ctx *app.Context, eng engine.Engine, branches engine.Branches, trunk engine.Branch, onProgress AnalysisProgressFunc) (*flattenPlan, error) {
	plan := &flattenPlan{
		Moves:       make([]basehandler.Reparent, 0),
		RebaseSpecs: make([]engine.RebaseSpec, 0),
	}

	// Resolve every branch and trunk revision, plus every divergence point, in
	// batched reads up front (passed-around values, no engine-global cache). The
	// revision map doubles as the parent-revision lookup the planning loop used
	// to accumulate one entry at a time.
	revNames := make([]string, 0, len(branches)+1)
	revNames = append(revNames, trunk.GetName())
	for _, b := range branches {
		revNames = append(revNames, b.GetName())
	}
	revisions, _ := eng.GetRevisions(revNames)
	divergence := eng.BatchDivergencePoints(branches)

	if revisions[trunk.GetName()] == "" {
		return nil, fmt.Errorf("failed to get trunk revision")
	}

	// potentialParents tracks branches that can serve as parents for subsequent branches
	// Starts with trunk, and grows as we process branches
	potentialParents := []string{trunk.GetName()}

	for i, b := range branches {
		bName := b.GetName()

		// Report progress
		if onProgress != nil {
			onProgress(i+1, len(branches), bName)
		}

		// Get current parent info
		origParentName := b.GetParentOrTrunk()

		// The branch's base (divergence point from parent)
		oldUpstream := divergence[bName]

		// Try to find the best (closest to trunk) parent this branch can move to
		newParent := findBestParent(ctx, eng, bName, oldUpstream, potentialParents, revisions)

		// Add this branch as a potential parent for subsequent branches
		potentialParents = append(potentialParents, bName)

		// If we found a better parent, add to the plan
		if newParent != "" && newParent != origParentName {
			plan.Moves = append(plan.Moves, basehandler.Reparent{
				Branch:    bName,
				OldParent: origParentName,
				NewParent: newParent,
			})

			// Add rebase spec for this move
			newParentRev := revisions[newParent]
			plan.RebaseSpecs = append(plan.RebaseSpecs, engine.RebaseSpec{
				Branch:      bName,
				NewParent:   newParentRev,
				OldUpstream: oldUpstream,
			})
		} else {
			plan.UnchangedCount++
		}
	}

	return plan, nil
}

// findBestParent finds the closest-to-trunk parent that the branch can cleanly rebase onto.
// Returns empty string if the branch should stay with its current parent.
func findBestParent(ctx *app.Context, eng engine.Engine, branchName, oldUpstream string, potentialParents []string, parentRevisions map[string]string) string {
	// Try each potential parent starting from trunk (index 0)
	// and working up through branches that have been processed
	for _, candidateParent := range potentialParents {
		candidateRev, ok := parentRevisions[candidateParent]
		if !ok {
			continue
		}

		// Quick check: if the candidate is already at the same revision as oldUpstream,
		// this branch is already optimally placed relative to this candidate
		if candidateRev == oldUpstream {
			return candidateParent
		}

		// Test if the branch can rebase onto this candidate
		if canRebaseOnto(ctx, eng, branchName, candidateRev, oldUpstream) {
			return candidateParent
		}
	}

	// No better parent found
	return ""
}

// canRebaseOnto tests if a branch can cleanly rebase onto a target.
// Uses ValidateRebases with a single spec to test in a temporary worktree.
func canRebaseOnto(ctx *app.Context, eng engine.Engine, branchName, targetRev, oldUpstream string) bool {
	specs := []engine.RebaseSpec{{
		Branch:      branchName,
		NewParent:   targetRev,
		OldUpstream: oldUpstream,
	}}

	validation, err := eng.ValidateRebases(ctx.Context, specs)
	if err != nil {
		ctx.Logger.Debug("Validation error for %s onto %s: %v", branchName, targetRev, err)
		return false
	}

	return validation.Success
}
