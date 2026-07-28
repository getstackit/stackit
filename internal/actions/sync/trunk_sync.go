package sync

import (
	"errors"
	"fmt"
	"time"

	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/internal/utils"
)

func syncFetchedTrunk(ctx *app.Context, opts *Options, handler Handler, summary *Summary) error {
	eng := ctx.Sync()
	gctx := ctx.Context

	// Capture trunk's tip before the pull so the report can say how far the
	// world moved, which is the context a developer reads a sync for.
	oldTrunkRev, _ := ctx.Navigator().Trunk().GetRevision()

	updateStart := time.Now()
	pullResult, err := eng.UpdateTrunkFromRemote(gctx)
	ctx.Logger.Info("update trunk from remote completed durationMs=%d", time.Since(updateStart).Milliseconds())
	if err != nil {
		// A worktree holding trunk is a hold, not a failure. Report it and let
		// the rest of the sync run: cleanup and restack do not depend on trunk
		// having moved, and aborting here strands the user in a directory whose
		// state they cannot see from the one they are standing in.
		var held *git.WorktreeHeldError
		if !errors.As(err, &held) {
			return fmt.Errorf("failed to update trunk from remote: %w", err)
		}
		handler.EmitEvent(Event{
			Phase:  PhaseTrunk,
			Type:   EventCompleted,
			Branch: held.Branch,
			HeldBy: fmt.Sprintf("worktree %s %s", held.WorktreePath, held.Reason),
		})
		return nil
	}

	return handleTrunkPullResult(ctx, opts, handler, summary, pullResult, oldTrunkRev)
}

func handleTrunkPullResult(ctx *app.Context, opts *Options, handler Handler, summary *Summary, pullResult engine.PullResult, oldTrunkRev string) error {
	eng := ctx.Sync()
	nav := ctx.Navigator()
	out := ctx.Output
	gctx := ctx.Context
	trunk := nav.Trunk()
	trunkName := trunk.GetName()

	switch pullResult {
	case engine.PullDone:
		trunk := nav.Trunk()
		rev, _ := trunk.GetRevision()
		revShort := utils.ShortRevision(rev, 0)
		summary.TrunkUpdated = true
		summary.TrunkRevision = revShort
		summary.TrunkCommits = trunkCommitsPulled(ctx, oldTrunkRev, rev)
		handler.EmitEvent(Event{
			Phase:       PhaseTrunk,
			Type:        EventCompleted,
			Branch:      trunkName,
			NewRevision: revShort,
			Commits:     summary.TrunkCommits,
		})
	case engine.PullUnneeded:
		handler.EmitEvent(Event{
			Phase:  PhaseTrunk,
			Type:   EventCompleted,
			Branch: trunkName,
		})
	case engine.PullConflict:
		// Prompt to overwrite (or use force flag)
		shouldReset := opts.Force
		if !shouldReset {
			// For now, if not force and interactive, we'll skip
			// In a full implementation, we would prompt here
			out.Warn("%s could not be fast-forwarded. Use --force to overwrite.", trunkName)
		}

		if shouldReset {
			resetStart := time.Now()
			if err := eng.ResetTrunkToRemote(gctx); err != nil {
				return fmt.Errorf("failed to reset trunk: %w", err)
			}
			ctx.Logger.Info("reset trunk to remote completed durationMs=%d", time.Since(resetStart).Milliseconds())
			trunk := nav.Trunk()
			rev, _ := trunk.GetRevision()
			revShort := utils.ShortRevision(rev, 0)
			summary.TrunkUpdated = true
			summary.TrunkRevision = revShort
			summary.TrunkCommits = trunkCommitsPulled(ctx, oldTrunkRev, rev)
			handler.EmitEvent(Event{
				Phase:       PhaseTrunk,
				Type:        EventCompleted,
				Branch:      trunkName,
				NewRevision: revShort,
				Commits:     summary.TrunkCommits,
			})
		}
	}

	return nil
}

// trunkCommitsPulled reports how many commits trunk advanced by. A failure to
// count is not worth failing a sync over — the report simply omits the delta.
func trunkCommitsPulled(ctx *app.Context, oldRev, newRev string) int {
	if oldRev == "" || newRev == "" || oldRev == newRev {
		return 0
	}
	count, err := ctx.Engine.CommitCountBetween(oldRev, newRev)
	if err != nil {
		ctx.Logger.Debug("count trunk commits pulled failed error=%v", err)
		return 0
	}
	return count
}
