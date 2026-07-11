package sync

import (
	"maps"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/getstackit/stackit/internal/actions"
	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/output"
)

// cleanBranches handles cleaning merged/closed branches
func cleanBranches(ctx *app.Context, opts *Options, dirtyAnchors dirtyAnchorSet, remoteStatuses engine.BranchRemoteStatuses, handler Handler, summary *Summary) (*actions.CleanBranchesResult, error) {
	// Only emit phase start if we have branches that might need cleaning
	allBranches := ctx.Engine.AllBranches()
	hasBranchesToCheck := false
	for _, b := range allBranches {
		if !b.IsTrunk() {
			hasBranchesToCheck = true
			break
		}
	}

	if hasBranchesToCheck {
		handler.EmitEvent(Event{Phase: PhaseClean, Type: EventStarted})
	}

	// Get current branch name for worktree detection
	currentBranchName := ""
	if currentBranch := ctx.Engine.CurrentBranch(); currentBranch != nil {
		currentBranchName = currentBranch.GetName()
	}

	cleanStart := time.Now()

	// Phase 1: Plan which branches to delete
	plan, err := actions.PlanBranchDeletions(ctx, actions.CleanBranchesOptions{
		Force:             opts.Force,
		InManagedWorktree: ctx.InManagedWorktree,
		CurrentBranch:     currentBranchName,
		RemoteStatuses:    remoteStatuses,
	})
	if err != nil {
		return nil, err
	}

	// Filter out branches in dirty stacks - don't delete them while worktree has uncommitted changes
	for name := range plan.BranchesToDelete {
		if dirtyAnchors.includes(ctx, name) {
			delete(plan.BranchesToDelete, name)
		}
	}

	// Log each branch and its deletion reason
	if len(plan.BranchesToDelete) > 0 {
		// Sort branch names for consistent logging
		names := make([]string, 0, len(plan.BranchesToDelete))
		for name := range plan.BranchesToDelete {
			names = append(names, name)
		}
		sort.Strings(names)

		// Build detailed log message
		var logDetails []string
		for _, name := range names {
			reason := plan.BranchesToDelete[name]
			logDetails = append(logDetails, name+": "+reason)
		}
		ctx.Logger.Info("branches planned for deletion count=%d branches=[%s]",
			len(plan.BranchesToDelete), strings.Join(logDetails, ", "))
	}

	// Phase 2: Build final deletion set.
	// Always pass an explicit filter to ExecuteBranchDeletions so any upstream
	// filtering (e.g. dirty stack exclusions) is enforced in both interactive
	// and non-interactive flows.
	branchesToDelete := make(map[string]bool, len(plan.BranchesToDelete))
	for name := range plan.BranchesToDelete {
		branchesToDelete[name] = true
	}
	if handler.IsInteractive() && len(plan.BranchesToDelete) > 0 {
		// Auto-confirm utility branches (e.g., consolidated merge branches)
		// These don't need user confirmation since their PRs are already closed/merged
		branchesToDelete = make(map[string]bool)
		branchesToPrompt := make(map[string]string)

		for name, reason := range plan.BranchesToDelete {
			if plan.UtilityBranches[name] && !plan.UnpushedBranches[name] {
				// Utility branch with no unpushed local changes - auto-confirm without prompting.
				branchesToDelete[name] = true
				ctx.Output.Info("Auto-deleting utility branch %s (%s)",
					output.BranchName(name), reason)
			} else {
				// Regular branch - add to prompt list
				branchesToPrompt[name] = reason
			}
		}

		// Prompt user for non-utility branches only
		if len(branchesToPrompt) > 0 {
			confirmed, err := handler.PromptBranchDeletions(branchesToPrompt, plan.UnpushedBranches)
			if err != nil {
				return nil, err
			}
			maps.Copy(branchesToDelete, confirmed)
		}
	} else if len(plan.UnpushedBranches) > 0 {
		// Non-interactive: skip unpushed branches by default
		for name := range plan.UnpushedBranches {
			delete(branchesToDelete, name)
		}
	}

	// Phase 3: Execute deletions
	result, err := actions.ExecuteBranchDeletions(ctx, plan, branchesToDelete)
	if err != nil {
		return nil, err
	}

	// Collect branches that were skipped due to unpushed changes
	for name := range plan.UnpushedBranches {
		if _, wasDeleted := result.DeletedBranches[name]; !wasDeleted {
			result.SkippedUnpushed = append(result.SkippedUnpushed, name)
		}
	}
	slices.Sort(result.SkippedUnpushed)

	ctx.Logger.Info("clean branches completed durationMs=%d deletedCount=%d", time.Since(cleanStart).Milliseconds(), len(result.DeletedBranches))

	// Emit events for each deleted branch
	for name, reason := range result.DeletedBranches {
		handler.EmitEvent(Event{
			Phase:   PhaseClean,
			Type:    EventCompleted,
			Branch:  name,
			Message: reason,
		})
		summary.BranchesDeleted++
	}

	// Warn about branches that couldn't be deleted from worktree
	for _, name := range result.SkippedInWorktree {
		ctx.Output.Warn("Cannot delete %s from worktree. Run sync from the main repository to clean up.",
			output.CurrentBranch(name))
	}

	// Warn about branches skipped due to unpushed changes
	for _, name := range result.SkippedUnpushed {
		ctx.Output.Warn("Skipped %s — has unpushed local changes. Push first or delete manually with 'git branch -D %s'.",
			output.BranchName(name), name)
	}

	return result, nil
}
