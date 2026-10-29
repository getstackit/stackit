package sync

import (
	"context"
	"time"

	"github.com/getstackit/stackit/internal/git"

	"github.com/getstackit/stackit/internal/engine"
)

// syncStackBranches pulls stack branches that are behind their remote counterparts.
// It skips branches that are trunk, locked, frozen, or in a dirty stack.
// This allows the sync command to fast-forward branches when someone else pushes
// commits to a stack branch from another machine.
func syncStackBranches(ctx context.Context, deps Deps, dirtyAnchors dirtyAnchorSet, remoteStatuses engine.BranchRemoteStatuses, handler Handler, summary *Summary) error {
	eng := deps.Engine
	remote := eng.GetRemote(ctx)

	// Emit phase started event
	handler.EmitEvent(Event{Phase: PhaseBranches, Type: EventStarted})

	// Get all tracked branches
	allBranches := eng.AllBranches()
	// Remote statuses are normally prefetched in sync's parallel phase so the
	// ls-remote overlaps the trunk fetch and GitHub call. Fall back to computing
	// them here when the caller didn't supply them.
	if remoteStatuses == nil {
		remoteCtx, cancelRemote := git.WithRemoteOperationTimeout(ctx)
		remoteStatuses = eng.ReadBranchRemoteStatuses(remoteCtx, allBranches)
		cancelRemote()
	}

	syncStart := time.Now()
	var statusCheckTotalMs int64
	statusCheckCount := 0
	branchesSynced := 0
	defer func() {
		deps.Log.Info("syncStackBranches completed durationMs=%d branchCount=%d branchesSynced=%d statusChecksMs=%d statusCheckCount=%d",
			time.Since(syncStart).Milliseconds(), len(allBranches), branchesSynced, statusCheckTotalMs, statusCheckCount)
	}()

	// Pass 1: collect branches that are behind their remote, applying the same
	// skip rules as before (trunk / untracked / locked / frozen / dirty stack).
	var behind engine.Branches
	for _, branch := range allBranches {
		// Check for context cancellation
		if err := ctx.Err(); err != nil {
			return context.Cause(ctx)
		}

		branchName := branch.Name()

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
		if dirtyAnchors.includes(eng, branchName) {
			continue
		}

		// Check if branch is behind remote
		statusStart := time.Now()
		status := remoteStatuses[branchName]
		statusCheckTotalMs += time.Since(statusStart).Milliseconds()
		statusCheckCount++

		// Skip if not behind remote
		if !status.Behind() {
			continue
		}

		behind = append(behind, branch)
	}

	if len(behind) == 0 {
		summary.BranchesSynced = 0
		return nil
	}

	// Fetch every behind branch in a single git fetch, replacing the previous
	// N per-branch fetches (one network round trip instead of N).
	behindNames := behind.Names()
	remoteCtx, cancelRemote := git.WithRemoteOperationTimeout(ctx)
	defer cancelRemote()
	fetchStart := time.Now()
	fetchErr := eng.FetchRemote(remoteCtx, engine.RemoteFetchRequest{
		Remote:   remote,
		Branches: behindNames,
	})
	deps.Log.Info("batch fetch stack branches completed durationMs=%d branchCount=%d fetchErr=%v",
		time.Since(fetchStart).Milliseconds(), len(behindNames), fetchErr)

	// Pass 2: apply the fast-forward for each branch. When the batch fetch
	// succeeded, UpdateBranchFromRemote does the local-only update (no network).
	// If the batch fetch failed, fall back to the per-branch PullBranch, which
	// re-fetches individually — preserving sync's warn-and-continue behavior
	// rather than failing the whole sync. Both paths run under the bounded
	// remoteCtx so a single deadline governs the whole pass.
	for _, branch := range behind {
		// Check for context cancellation
		if err := ctx.Err(); err != nil {
			return context.Cause(ctx)
		}

		branchName := branch.Name()

		pullStart := time.Now()
		var result engine.PullResult
		var err error
		if fetchErr != nil {
			result, err = eng.PullBranch(remoteCtx, remote, branchName)
		} else {
			result, err = eng.UpdateBranchFromRemote(remoteCtx, remote, branchName)
		}
		deps.Log.Info("update branch from remote completed branch=%v durationMs=%v", branchName, time.Since(pullStart).Milliseconds())

		if applyBranchSyncResult(ctx, branch, result, err, handler, summary) {
			branchesSynced++
		}
	}

	summary.BranchesSynced = branchesSynced
	return nil
}

// applyBranchSyncResult emits the PhaseBranches event for a single branch's
// pull/update result and reports whether the branch was fast-forwarded. It is
// shared by the batched local-update path and the per-branch fetch fallback so
// both behave identically.
func applyBranchSyncResult(ctx context.Context, branch engine.Branch, result engine.PullResult, err error, handler Handler, summary *Summary) (synced bool) {
	branchName := branch.Name()

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
		return false
	}

	switch result {
	case engine.PullDone:
		// Get the new revision (GetRevision reads live git, so it reflects the
		// just-applied fast-forward without an engine rebuild).
		rev, _ := branch.Revision(ctx)
		revShort := git.ShortRevision(rev, 0)
		handler.EmitEvent(Event{
			Phase:       PhaseBranches,
			Type:        EventCompleted,
			Branch:      branchName,
			NewRevision: revShort,
		})
		return true

	case engine.PullUnneeded:
		// Already up to date (shouldn't happen since we checked Behind(), but handle it)
		handler.EmitEvent(Event{
			Phase:  PhaseBranches,
			Type:   EventCompleted,
			Branch: branchName,
		})
		return false

	case engine.PullConflict:
		// Branches have diverged
		summary.ConflictBranches = append(summary.ConflictBranches, branchName)
		handler.EmitEvent(Event{
			Phase:    PhaseBranches,
			Type:     EventSkipped,
			Branch:   branchName,
			Conflict: true,
		})
		return false
	}

	return false
}
