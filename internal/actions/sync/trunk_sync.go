package sync

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/git"
)

func syncFetchedTrunk(ctx context.Context, deps Deps, opts *Options, handler Handler, summary *Summary) error {
	eng := deps.Engine

	// Capture trunk's tip before the pull so the report can say how far the
	// world moved, which is the context a developer reads a sync for.
	// Without it the report omits the delta; the sync itself is unaffected.
	oldTrunkRev, err := eng.Trunk().Revision(ctx)
	if err != nil {
		deps.Log.Debug("read trunk revision before pull failed error=%v", err)
	}

	updateStart := time.Now()
	pullResult, err := eng.UpdateTrunkFromRemote(ctx)
	deps.Log.Info("update trunk from remote completed durationMs=%d", time.Since(updateStart).Milliseconds())
	if err != nil {
		// A worktree holding trunk is a hold, not a failure. Report it and let
		// the rest of the sync run: cleanup and restack do not depend on trunk
		// having moved, and aborting here strands the user in a directory whose
		// state they cannot see from the one they are standing in.
		held, ok := errors.AsType[*git.WorktreeHeldError](err)
		if !ok {
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

	return handleTrunkPullResult(ctx, deps, opts, handler, summary, pullResult, oldTrunkRev)
}

func handleTrunkPullResult(ctx context.Context, deps Deps, opts *Options, handler Handler, summary *Summary, pullResult engine.PullResult, oldTrunkRev string) error {
	eng := deps.Engine
	out := deps.Out
	trunk := eng.Trunk()
	trunkName := trunk.Name()

	switch pullResult {
	case engine.PullDone:
		trunk := eng.Trunk()
		rev, _ := trunk.Revision(ctx)
		revShort := git.ShortRevision(rev, 0)
		summary.TrunkUpdated = true
		summary.TrunkRevision = revShort
		summary.TrunkCommits = trunkCommitsPulled(ctx, deps, oldTrunkRev, rev)
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
			if err := eng.ResetTrunkToRemote(ctx); err != nil {
				return fmt.Errorf("failed to reset trunk: %w", err)
			}
			deps.Log.Info("reset trunk to remote completed durationMs=%d", time.Since(resetStart).Milliseconds())
			trunk := eng.Trunk()
			rev, _ := trunk.Revision(ctx)
			revShort := git.ShortRevision(rev, 0)
			summary.TrunkUpdated = true
			summary.TrunkRevision = revShort
			summary.TrunkCommits = trunkCommitsPulled(ctx, deps, oldTrunkRev, rev)
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
func trunkCommitsPulled(ctx context.Context, deps Deps, oldRev, newRev string) int {
	if oldRev == "" || newRev == "" || oldRev == newRev {
		return 0
	}
	count, err := deps.Engine.CommitCountBetween(ctx, git.RevRange{Base: oldRev, Head: newRev})
	if err != nil {
		deps.Log.Debug("count trunk commits pulled failed error=%v", err)
		return 0
	}
	return count
}
