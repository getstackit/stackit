package merge

import (
	mergeAction "github.com/getstackit/stackit/internal/actions/merge"
	"github.com/getstackit/stackit/internal/output"
)

// Merge strategy string constants shared across merge subcommands.
const (
	mergeStrategyShip     = "ship"
	mergeStrategyBottomUp = "bottom-up"
	mergeStrategyDone     = "done"

	shipAliasSquash = "squash"
)

// progressRenderer renders MergePR and Drain progress events to the terminal.
type progressRenderer struct {
	out output.Output
}

func newProgressRenderer(out output.Output) *progressRenderer {
	return &progressRenderer{out: out}
}

// OnProgress implements mergeAction.ProgressHandler.
func (r *progressRenderer) OnProgress(event mergeAction.ProgressEvent) {
	out := r.out
	switch ev := event.(type) {
	case mergeAction.PRAlreadyMergedEvent:
		out.Success("PR #%d is already merged", ev.PRNumber)
	case mergeAction.PRMergingDirectlyEvent:
		if ev.AfterWaiting {
			out.Info("PR #%d is now %s — merging directly (method: %s)...", ev.PRNumber, ev.MergeState, ev.Method)
		} else {
			out.Info("PR #%d is ready to merge — merging directly (method: %s)...", ev.PRNumber, ev.Method)
		}
	case mergeAction.PRMergedEvent:
		if ev.External {
			out.Success("PR #%d was merged externally!", ev.PRNumber)
		} else {
			out.Success("PR #%d merged successfully!", ev.PRNumber)
		}
	case mergeAction.PREnablingAutomergeEvent:
		out.Info("Enabling automerge on PR #%d (method: %s)...", ev.PRNumber, ev.Method)
	case mergeAction.PRAutomergeEnabledEvent:
		out.Success("Automerge enabled on PR #%d", ev.PRNumber)
	case mergeAction.PRWaitingForMergeEvent:
		out.Info("Waiting for PR #%d to be merged...", ev.PRNumber)
	case mergeAction.PRWaitingForMergeableEvent:
		out.Info("Auto-merge not enabled on this repo — waiting for PR #%d to become mergeable...", ev.PRNumber)
	case mergeAction.PRCleanStatusRaceEvent:
		out.Debug("Automerge returned clean status — PR became ready, merging directly")
	case mergeAction.DrainPRStartedEvent:
		out.Newline()
		out.Info("Merging PR #%d (%s) [%d/%d]...", ev.PRNumber, ev.BranchName, ev.Position, ev.Total)
	case mergeAction.DrainSyncingEvent:
		out.Info("Syncing trunk and restacking...")
	case mergeAction.DrainBranchPushedEvent:
		if ev.Failed {
			out.Warn("  ✗ %s failed to update: %v", ev.BranchName, ev.Err)
		} else {
			out.Info("  ✓ %s updated → %s", ev.BranchName, ev.URL)
		}
	case mergeAction.DrainStoppedEvent:
		out.Debug("Stopping drain after %d merges: %v", ev.Merged, ev.Reason)
	}
}
