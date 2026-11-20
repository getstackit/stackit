package sync

import (
	"context"
	"time"

	"github.com/getstackit/stackit/internal/xslices"

	"github.com/getstackit/stackit/internal/actions"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/rerere"
)

// restackBranches restacks branches after sync operations through the shared
// restack pipeline, folding its outcome into the sync summary.
// When restackScope is non-nil, only those branches are restacked (skipping current-branch expansion).
// When expandScope is true, expands to the full current stack (used when --restack is explicitly passed).
func restackBranches(ctx context.Context, deps Deps, branchesToRestack []string, restackScope []string, expandScope bool, dirtyAnchors dirtyAnchorSet, handler Handler, summary *Summary) (actions.RestackOutcome, error) {
	eng := deps.Engine

	if restackScope != nil {
		// Scoped restack: use only the explicitly provided branches
		branchesToRestack = append(branchesToRestack, restackScope...)
	} else if expandScope {
		// Explicit --restack: expand from current branch position to full stack
		graph := eng.Graph(ctx, engine.SortStrategyAlphabetical)

		currentBranch := eng.CurrentBranch(ctx)
		if currentBranch != nil {
			if currentBranch.IsTracked() {
				// Get full stack (up to trunk)
				stack := graph.Range(*currentBranch, engine.StackRange{
					RecursiveParents:  true,
					IncludeCurrent:    true,
					RecursiveChildren: true,
				})
				// Add branches to restack list
				for _, b := range stack {
					branchesToRestack = append(branchesToRestack, b.Name())
				}
			} else if currentBranch.IsTrunk() {
				// If on trunk, restack all branches
				stack := graph.Range(*currentBranch, engine.StackRange{
					RecursiveChildren: true,
				})
				for _, b := range stack {
					branchesToRestack = append(branchesToRestack, b.Name())
				}
			}
		}
	}
	// When expandScope is false and restackScope is nil, only restack
	// branches already in branchesToRestack (reparented branches from sync)

	// Remove duplicates and filter out non-existent/untracked branches and dirty stacks
	uniqueBranches := engine.NewBranchesBuilder(len(branchesToRestack))
	for _, branchName := range xslices.Unique(branchesToRestack) {
		if dirtyAnchors.includes(eng, branchName) {
			continue
		}
		// Only include branches that exist, are tracked, and are not trunks
		if branch := eng.GetBranch(branchName); branch.IsTracked() && !branch.IsTrunk() {
			uniqueBranches.Add(branch)
		}
	}

	// Sort branches topologically (parents before children) for correct restack order
	sortedBranches := eng.SortBranchesTopologically(ctx, uniqueBranches.Build())

	if len(sortedBranches) == 0 {
		return actions.RestackOutcome{}, nil
	}

	// Sync opens the restack phase and reports its own summary, so only the
	// per-branch events reach the handler.
	pauser, _ := handler.(rerere.Pauser)
	restackStart := time.Now()
	outcome, err := actions.RunRestack(ctx, deps.Restack, actions.RestackRequest{
		Branches:       sortedBranches,
		ConflictPolicy: actions.RestackConflictPrompt,
		Prompter:       handler,
		PromptRerere:   deps.Restack.Interactive && !deps.Restack.Quiet && handler.IsInteractive(),
		Pauser:         pauser,
	}, actions.RestackBranchFunc(func(event actions.RestackBranchEvent) {
		// Reparenting travels with every outcome: a branch can be reparented
		// and rebased, or reparented and already current.
		if event.Reparented {
			summary.BranchesReparented++
		}
		handler.OnRestackBranch(event)
	}))

	summary.BranchesRestacked += outcome.Summary.Restacked
	summary.BranchesSkipped += len(outcome.Summary.Conflicts)
	summary.ConflictBranches = append(summary.ConflictBranches, outcome.Summary.Conflicts...)
	summary.BranchesBlocked += len(outcome.Summary.Blocked)
	deps.Log.Info("restack branches completed durationMs=%d branchCount=%d", time.Since(restackStart).Milliseconds(), len(sortedBranches))

	return outcome, err
}
