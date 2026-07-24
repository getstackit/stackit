package stack

import (
	"time"

	syncAction "github.com/getstackit/stackit/internal/actions/sync"
)

// SyncScenario is a deterministic replay of the action events emitted by sync.
// It drives the production Handler, not the Bubble Tea model directly, so its
// terminal lifecycle and event formatting match the sync command.
type SyncScenario struct {
	Name        string
	Description string
	TotalOps    int
	Events      []syncAction.Event
	Summary     syncAction.Summary
}

// Replay sends this scenario through a sync handler. delay makes individual
// command states visible in the terminal; zero is useful for automated checks.
func (s SyncScenario) Replay(handler syncAction.Handler, delay time.Duration) {
	handler.Start(s.TotalOps)
	pause(delay)
	for _, event := range s.Events {
		handler.EmitEvent(event)
		pause(delay)
	}
	handler.Complete(s.Summary)
}

// SyncScenarios are the named sync flows available in the local TUI lab.
var SyncScenarios = []SyncScenario{
	{
		Name:        "success",
		Description: "Trunk update, PR refresh, cleanup, and restack.",
		TotalOps:    6,
		Events: []syncAction.Event{
			{Phase: syncAction.PhaseTrunk, Type: syncAction.EventStarted},
			{Phase: syncAction.PhaseGitHub, Type: syncAction.EventStarted},
			{Phase: syncAction.PhaseTrunk, Type: syncAction.EventCompleted, Branch: "main", NewRevision: "a1b2c3d"},
			{Phase: syncAction.PhaseGitHub, Type: syncAction.EventProgress, Branch: "feat/api"},
			{Phase: syncAction.PhaseGitHub, Type: syncAction.EventCompleted, Message: "Updated PR #42 for feat/api"},
			{Phase: syncAction.PhaseBranches, Type: syncAction.EventStarted},
			{Phase: syncAction.PhaseBranches, Type: syncAction.EventCompleted, Branch: "feat/api"},
			{Phase: syncAction.PhaseClean, Type: syncAction.EventStarted},
			{Phase: syncAction.PhaseClean, Type: syncAction.EventCompleted, Branch: "fix/merged", PRNumber: new(39), Message: "after merge"},
			{Phase: syncAction.PhaseRestack, Type: syncAction.EventStarted},
			{Phase: syncAction.PhaseRestack, Type: syncAction.EventCompleted, Branch: "feat/web", PRNumber: new(43), Parent: "feat/api", NewRevision: "d4e5f6a"},
		},
		Summary: syncAction.Summary{
			TrunkUpdated:      true,
			BranchesSynced:    1,
			BranchesDeleted:   1,
			BranchesRestacked: 1,
		},
	},
	{
		Name:        "diverged",
		Description: "A remote branch diverges and an affected restack conflicts.",
		TotalOps:    3,
		Events: []syncAction.Event{
			{Phase: syncAction.PhaseTrunk, Type: syncAction.EventStarted},
			{Phase: syncAction.PhaseGitHub, Type: syncAction.EventStarted},
			{Phase: syncAction.PhaseTrunk, Type: syncAction.EventCompleted, Branch: "main"},
			{Phase: syncAction.PhaseBranches, Type: syncAction.EventStarted},
			{Phase: syncAction.PhaseBranches, Type: syncAction.EventSkipped, Branch: "feat/api", Conflict: true},
			{Phase: syncAction.PhaseRestack, Type: syncAction.EventStarted},
			{Phase: syncAction.PhaseRestack, Type: syncAction.EventSkipped, Branch: "feat/web", PRNumber: new(43), Conflict: true},
		},
		Summary: syncAction.Summary{
			BranchesSkipped:  2,
			ConflictBranches: []string{"feat/web"},
		},
	},
	{
		Name:        "current",
		Description: "The lightweight no-op path where trunk is already current.",
		TotalOps:    1,
		Events: []syncAction.Event{
			{Phase: syncAction.PhaseTrunk, Type: syncAction.EventStarted},
			{Phase: syncAction.PhaseGitHub, Type: syncAction.EventStarted},
			{Phase: syncAction.PhaseTrunk, Type: syncAction.EventCompleted, Branch: "main"},
		},
		Summary: syncAction.Summary{UpToDate: true},
	},
}

// LookupSyncScenario returns a named sync scenario.
func LookupSyncScenario(name string) (SyncScenario, bool) {
	for _, scenario := range SyncScenarios {
		if scenario.Name == name {
			return scenario, true
		}
	}
	return SyncScenario{}, false
}

// SyncScenarioNames returns scenario names in display order.
func SyncScenarioNames() []string {
	names := make([]string, 0, len(SyncScenarios))
	for _, scenario := range SyncScenarios {
		names = append(names, scenario.Name)
	}
	return names
}

func pause(delay time.Duration) {
	if delay > 0 {
		time.Sleep(delay)
	}
}
