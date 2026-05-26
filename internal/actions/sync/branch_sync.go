package sync

import (
	"context"
	"time"

	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/git"
)

// syncStackBranches pulls stack branches that are behind their remote counterparts.
// It skips branches that are trunk, locked, frozen, or in a dirty stack.
// This allows the sync command to fast-forward branches when someone else pushes
// commits to a stack branch from another machine.
func syncStackBranches(ctx *app.Context, dirtyAnchors map[string]bool, handler Handler, summary *Summary) error {
	eng := ctx.Engine
	nav := ctx.Navigator()
	gctx := ctx.Context
	remote := eng.GetRemote()

	// Emit phase started event
	handler.EmitEvent(Event{Phase: PhaseBranches, Type: EventStarted})

	// Get all tracked branches
	allBranches := nav.AllBranches()

	syncStart := time.Now()
	var statusCheckTotalMs int64
	statusCheckCount := 0
	branchesSynced := 0
	defer func() {
		ctx.Logger.Info("syncStackBranches completed durationMs=%d branchCount=%d branchesSynced=%d statusChecksMs=%d statusCheckCount=%d",
			time.Since(syncStart).Milliseconds(), len(allBranches), branchesSynced, statusCheckTotalMs, statusCheckCount)
	}()

	for _, branch := range allBranches {
		// Check for context cancellation
		if err := gctx.Err(); err != nil {
			return context.Cause(gctx)
		}

		branchName := branch.GetName()

		// Skip trunk
		if eng.IsTrunk(branch) {
			continue
		}

		// Skip untracked branches
		if !eng.IsTracked(branch) {
			continue
		}

		// Skip locked branches
		if eng.IsLocked(branch) {
			continue
		}

		// Skip frozen branches
		if eng.IsFrozen(branch) {
			continue
		}

		// Skip branches in dirty stacks
		if isInDirtyStack(ctx, branchName, dirtyAnchors) {
			continue
		}

		// Check if branch is behind remote
		statusStart := time.Now()
		status, err := eng.GetBranchRemoteStatus(branch)
		statusCheckTotalMs += time.Since(statusStart).Milliseconds()
		statusCheckCount++
		if err != nil {
			// Can't determine status, skip
			continue
		}

		// Skip if not behind remote
		if !status.Behind() {
			continue
		}

		// Pull the branch
		pullStart := time.Now()
		result, err := ctx.Git().PullBranch(gctx, remote, branchName)
		ctx.Logger.Info("pull branch completed branch=%v durationMs=%v", branchName, time.Since(pullStart).Milliseconds())

		if err != nil {
			// Treat errors as conflicts - warn and continue
			summary.ConflictBranches = append(summary.ConflictBranches, branchName)
			handler.EmitEvent(Event{
				Phase:    PhaseBranches,
				Type:     EventSkipped,
				Branch:   branchName,
				Conflict: true,
				Message:  err.Error(),
			})
			continue
		}

		switch result {
		case git.PullDone:
			// Get the new revision
			rev, _ := branch.GetRevision()
			revShort := rev
			if len(rev) > 7 {
				revShort = rev[:7]
			}
			branchesSynced++
			handler.EmitEvent(Event{
				Phase:       PhaseBranches,
				Type:        EventCompleted,
				Branch:      branchName,
				NewRevision: revShort,
			})

		case git.PullUnneeded:
			// Already up to date (shouldn't happen since we checked Behind(), but handle it)
			handler.EmitEvent(Event{
				Phase:  PhaseBranches,
				Type:   EventCompleted,
				Branch: branchName,
			})

		case git.PullConflict:
			// Branches have diverged
			summary.ConflictBranches = append(summary.ConflictBranches, branchName)
			handler.EmitEvent(Event{
				Phase:    PhaseBranches,
				Type:     EventSkipped,
				Branch:   branchName,
				Conflict: true,
			})
		}
	}

	summary.BranchesSynced = branchesSynced
	return nil
}
