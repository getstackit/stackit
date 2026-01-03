package sync

import (
	"fmt"

	"stackit.dev/stackit/internal/actions"
	"stackit.dev/stackit/internal/app"
	"stackit.dev/stackit/internal/engine"
)

// restackBranches handles restacking branches after sync operations
func restackBranches(ctx *app.Context, branchesToRestack []string, handler Handler, summary *Summary) error {
	nav := ctx.Navigator()

	// Add current branch stack to restack list
	currentBranch := nav.CurrentBranch()
	if currentBranch != nil {
		if currentBranch.IsTracked() {
			// Get full stack (up to trunk)
			stack := currentBranch.GetFullStack()
			// Add branches to restack list
			for _, b := range stack {
				branchesToRestack = append(branchesToRestack, b.GetName())
			}
		} else if currentBranch.IsTrunk() {
			// If on trunk, restack all branches
			stack := currentBranch.GetRelativeStack(engine.StackRange{RecursiveChildren: true})
			for _, b := range stack {
				branchesToRestack = append(branchesToRestack, b.GetName())
			}
		}
	}

	// Remove duplicates and filter out non-existent/untracked branches
	seen := make(map[string]bool)
	uniqueBranches := []engine.Branch{}
	for _, branchName := range branchesToRestack {
		if !seen[branchName] {
			seen[branchName] = true
			branch := nav.GetBranch(branchName)
			// Only include branches that exist and are tracked
			if branch.IsTracked() {
				uniqueBranches = append(uniqueBranches, branch)
			}
		}
	}

	// Sort branches topologically (parents before children) for correct restack order
	sortedBranches := nav.SortBranchesTopologically(uniqueBranches)

	// Restack branches with handler for progress
	if len(sortedBranches) > 0 {
		if err := actions.RestackBranchesWithHandler(ctx, sortedBranches, func(branchName string, result engine.RestackResult, newRev string, _ bool, lockReason engine.LockReason, frozen bool) {
			prNumber := getPRNumber(ctx.Status(), branchName)

			switch result {
			case engine.RestackDone:
				summary.BranchesRestacked++
				handler.EmitEvent(Event{
					Phase:       PhaseRestack,
					Type:        EventCompleted,
					Branch:      branchName,
					PRNumber:    prNumber,
					NewRevision: newRev,
					LockReason:  lockReason,
					Frozen:      frozen,
				})
			case engine.RestackUnneeded:
				handler.EmitEvent(Event{
					Phase:      PhaseRestack,
					Type:       EventCompleted,
					Branch:     branchName,
					PRNumber:   prNumber,
					LockReason: lockReason,
					Frozen:     frozen,
				})
			case engine.RestackConflict:
				summary.BranchesSkipped++
				summary.ConflictBranches = append(summary.ConflictBranches, branchName)
				handler.EmitEvent(Event{
					Phase:      PhaseRestack,
					Type:       EventSkipped,
					Branch:     branchName,
					PRNumber:   prNumber,
					Conflict:   true,
					LockReason: lockReason,
					Frozen:     frozen,
				})
			}
		}); err != nil {
			return fmt.Errorf("failed to restack branches: %w", err)
		}
	}

	return nil
}

// getPRNumber returns the PR number for a branch if it has one
func getPRNumber(eng engine.BranchStatus, branchName string) *int {
	branch := eng.GetBranch(branchName)
	prInfo, err := eng.GetPrInfo(branch)
	if err != nil || prInfo == nil {
		return nil
	}
	return prInfo.Number()
}
