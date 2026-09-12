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
		summary.recordHold(held.Branch)
		handler.EmitEvent(Event{
			Phase:  PhaseTrunk,
			Type:   EventCompleted,
			Branch: held.Branch,
			HeldBy: fmt.Sprintf("worktree %s %s", held.WorktreePath, held.Reason),
		})
		return nil
	}

	return handleTrunkPullResult(ctx, deps, opts, handler, summary, pullResult)
}

func handleTrunkPullResult(ctx context.Context, deps Deps, opts *Options, handler Handler, summary *Summary, pullResult engine.PullResult) error {
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
		handler.EmitEvent(Event{
			Phase:       PhaseTrunk,
			Type:        EventCompleted,
			Branch:      trunkName,
			NewRevision: revShort,
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
			handler.EmitEvent(Event{
				Phase:       PhaseTrunk,
				Type:        EventCompleted,
				Branch:      trunkName,
				NewRevision: revShort,
			})
		}
	}

	return nil
}
