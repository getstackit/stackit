package main

import (
	"time"

	syncAction "github.com/getstackit/stackit/internal/actions/sync"
)

func intPointer(n int) *int { return &n }

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
			{Phase: syncAction.PhaseTrunk, Type: syncAction.EventCompleted, Branch: "main", NewRevision: "a1b2c3d", Commits: 12},
			{Phase: syncAction.PhaseGitHub, Type: syncAction.EventProgress, Branch: "feat/api"},
			{Phase: syncAction.PhaseGitHub, Type: syncAction.EventCompleted, Message: "Updated PR #42 for feat/api"},
			{Phase: syncAction.PhaseBranches, Type: syncAction.EventStarted},
			{Phase: syncAction.PhaseBranches, Type: syncAction.EventCompleted, Branch: "feat/api", NewRevision: "e4f5a6b"},
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
		Name:        "large-stack",
		Description: "A multi-branch sync with merged cleanup and one restack conflict.",
		TotalOps:    11,
		Events: []syncAction.Event{
			{Phase: syncAction.PhaseTrunk, Type: syncAction.EventStarted},
			{Phase: syncAction.PhaseGitHub, Type: syncAction.EventStarted},
			{Phase: syncAction.PhaseTrunk, Type: syncAction.EventCompleted, Branch: "main"},
			{Phase: syncAction.PhaseGitHub, Type: syncAction.EventCompleted, Message: "Refreshed PR info for 6 branches"},
			{Phase: syncAction.PhaseClean, Type: syncAction.EventStarted},
			{Phase: syncAction.PhaseClean, Type: syncAction.EventCompleted, Branch: "stack-merge-stack-1784862381", Message: "merged into main"},
			{Phase: syncAction.PhaseClean, Type: syncAction.EventCompleted, Branch: "jonnii/20260724022739/make-restack-output-terse-and-outcome-focused", Message: "merged into main"},
			{Phase: syncAction.PhaseRestack, Type: syncAction.EventStarted},
			{Phase: syncAction.PhaseRestack, Type: syncAction.EventCompleted, Branch: "info-query-cli-rendering", PRNumber: intPointer(936), Parent: "main", NewRevision: "9e49378"},
			{Phase: syncAction.PhaseRestack, Type: syncAction.EventCompleted, Branch: "jonnii/20260529015605/add-user-menu-to-header-and-extend-it-to-repo", PRNumber: intPointer(1070), Parent: "main", NewRevision: "15d7955"},
			{Phase: syncAction.PhaseRestack, Type: syncAction.EventCompleted, Branch: "jonnii/20260605031052/take-rename-s-prompt-off-the-TUI-drop-tui-from", PRNumber: intPointer(1164), Parent: "main", NewRevision: "b0d42a4"},
			{Phase: syncAction.PhaseRestack, Type: syncAction.EventCompleted, Branch: "jonnii/20260605032227/extract-OpenEditor-into-internal/editor-out-of", PRNumber: intPointer(1242), Parent: "jonnii/20260605031052/take-rename-s-prompt-off-the-TUI-drop-tui-from", NewRevision: "e1d9f30"},
			{Phase: syncAction.PhaseRestack, Type: syncAction.EventCompleted, Branch: "jonnii/20260605033020/route-insert-s-child-select-prompt-through-the", PRNumber: intPointer(1241), Parent: "jonnii/20260605032227/extract-OpenEditor-into-internal/editor-out-of", NewRevision: "4cb45e0"},
			{Phase: syncAction.PhaseRestack, Type: syncAction.EventCompleted, Branch: "jonnii/20260219034124/prompt-notes-wt"},
			{Phase: syncAction.PhaseRestack, Type: syncAction.EventSkipped, Branch: "jonnii/20260220125253/add-prompt-notes-to-track-LLM-context-on-commits", PRNumber: intPointer(754), Conflict: true},
		},
		Summary: syncAction.Summary{
			BranchesDeleted:   2,
			BranchesRestacked: 5,
			BranchesSkipped:   1,
			ConflictBranches:  []string{"jonnii/20260220125253/add-prompt-notes-to-track-LLM-context-on-commits"},
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
