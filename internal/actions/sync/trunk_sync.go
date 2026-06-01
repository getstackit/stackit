package sync

import (
	"fmt"
	"time"

	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/engine"
)

func syncFetchedTrunk(ctx *app.Context, opts *Options, handler Handler, summary *Summary) error {
	eng := ctx.Sync()
	gctx := ctx.Context

	updateStart := time.Now()
	pullResult, err := eng.UpdateTrunkFromRemote(gctx)
	ctx.Logger.Info("update trunk from remote completed durationMs=%d", time.Since(updateStart).Milliseconds())
	if err != nil {
		return fmt.Errorf("failed to update trunk from remote: %w", err)
	}

	return handleTrunkPullResult(ctx, opts, handler, summary, pullResult)
}

func handleTrunkPullResult(ctx *app.Context, opts *Options, handler Handler, summary *Summary, pullResult engine.PullResult) error {
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
		revShort := rev
		if len(rev) > 7 {
			revShort = rev[:7]
		}
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
			if err := eng.ResetTrunkToRemote(gctx); err != nil {
				return fmt.Errorf("failed to reset trunk: %w", err)
			}
			ctx.Logger.Info("reset trunk to remote completed durationMs=%d", time.Since(resetStart).Milliseconds())
			trunk := nav.Trunk()
			rev, _ := trunk.GetRevision()
			revShort := rev
			if len(rev) > 7 {
				revShort = rev[:7]
			}
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
