package sync

import (
	"fmt"

	"stackit.dev/stackit/internal/app"
	"stackit.dev/stackit/internal/engine"
)

// syncTrunk handles pulling the trunk and resolving any conflicts
func syncTrunk(ctx *app.Context, opts *Options, handler Handler, summary *Summary) error {
	eng := ctx.Sync()
	nav := ctx.Navigator()
	splog := ctx.Splog
	gctx := ctx.Context
	trunk := nav.Trunk()
	trunkName := trunk.GetName()

	pullResult, err := eng.PullTrunk(gctx)
	if err != nil {
		return fmt.Errorf("failed to pull trunk: %w", err)
	}

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
			splog.Warn("%s could not be fast-forwarded. Use --force to overwrite.", trunkName)
		}

		if shouldReset {
			if err := eng.ResetTrunkToRemote(gctx); err != nil {
				return fmt.Errorf("failed to reset trunk: %w", err)
			}
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
