package main

import (
	"time"

	"github.com/getstackit/stackit/internal/actions"
	syncAction "github.com/getstackit/stackit/internal/actions/sync"
	"github.com/getstackit/stackit/internal/git"
)

func prNumberPointer(n git.PRNumber) *git.PRNumber { return &n }

// SyncScenario is a deterministic replay of the action events emitted by sync.
// It drives the production Handler, not the Bubble Tea model directly, so its
// terminal lifecycle and event formatting match the sync command.
type SyncScenario struct {
	Name        string
	Description string
	Steps       []SyncStep
	Summary     syncAction.Summary
}

// SyncStep is one event sync emits: a phase event, or a per-branch restack
// event from the shared restack pipeline. Exactly one field is set.
type SyncStep struct {
	Event   *syncAction.Event
	Restack *actions.RestackBranchEvent
}

func syncEvent(e syncAction.Event) SyncStep { return SyncStep{Event: &e} }

func restackEvent(e actions.RestackBranchEvent) SyncStep { return SyncStep{Restack: &e} }

// Replay sends this scenario through a sync handler. delay makes individual
// command states visible in the terminal; zero is useful for automated checks.
func (s SyncScenario) Replay(handler syncAction.Handler, delay time.Duration) {
	handler.Start()
	pause(delay)
	for _, step := range s.Steps {
		if step.Restack != nil {
			handler.OnRestackBranch(*step.Restack)
		} else {
			handler.EmitEvent(*step.Event)
		}
		pause(delay)
	}
	handler.Complete(s.Summary)
}

// SyncScenarios are the named sync flows available in the local TUI lab.
var SyncScenarios = []SyncScenario{
	{
		Name:        "success",
		Description: "Trunk update, PR refresh, cleanup, and restack.",
		Steps: []SyncStep{
			syncEvent(syncAction.Event{Phase: syncAction.PhaseTrunk, Type: syncAction.EventStarted}),
			syncEvent(syncAction.Event{Phase: syncAction.PhaseGitHub, Type: syncAction.EventStarted}),
			syncEvent(syncAction.Event{Phase: syncAction.PhaseTrunk, Type: syncAction.EventCompleted, Branch: "main", NewRevision: "a1b2c3d"}),
			syncEvent(syncAction.Event{Phase: syncAction.PhaseGitHub, Type: syncAction.EventCompleted, Message: "Updated PR info for 1 branch"}),
			syncEvent(syncAction.Event{Phase: syncAction.PhaseGitHub, Type: syncAction.EventProgress, Message: "Updating PR metadata for 1 branch..."}),
			syncEvent(syncAction.Event{Phase: syncAction.PhaseGitHub, Type: syncAction.EventCompleted, Message: "Updated PR metadata for 1 branch"}),
			syncEvent(syncAction.Event{Phase: syncAction.PhaseBranches, Type: syncAction.EventStarted}),
			syncEvent(syncAction.Event{Phase: syncAction.PhaseBranches, Type: syncAction.EventCompleted, Branch: "feat/api"}),
			syncEvent(syncAction.Event{Phase: syncAction.PhaseClean, Type: syncAction.EventStarted}),
			syncEvent(syncAction.Event{Phase: syncAction.PhaseClean, Type: syncAction.EventCompleted, Branch: "fix/merged", PRNumber: new(git.PRNumber(39)), Message: "after merge"}),
			syncEvent(syncAction.Event{Phase: syncAction.PhaseRestack, Type: syncAction.EventStarted}),
			restackEvent(actions.RestackBranchEvent{Result: actions.RestackDone, Branch: "feat/web", PRNumber: new(git.PRNumber(43)), Parent: "feat/api", NewRevision: "d4e5f6a"}),
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
		Steps: []SyncStep{
			syncEvent(syncAction.Event{Phase: syncAction.PhaseTrunk, Type: syncAction.EventStarted}),
			syncEvent(syncAction.Event{Phase: syncAction.PhaseGitHub, Type: syncAction.EventStarted}),
			syncEvent(syncAction.Event{Phase: syncAction.PhaseTrunk, Type: syncAction.EventCompleted, Branch: "main"}),
			syncEvent(syncAction.Event{Phase: syncAction.PhaseGitHub, Type: syncAction.EventCompleted, Message: "Updated PR info for 6 branches"}),
			syncEvent(syncAction.Event{Phase: syncAction.PhaseClean, Type: syncAction.EventStarted}),
			syncEvent(syncAction.Event{Phase: syncAction.PhaseClean, Type: syncAction.EventCompleted, Branch: "stack-merge-stack-1784862381", Message: "merged into main"}),
			syncEvent(syncAction.Event{Phase: syncAction.PhaseClean, Type: syncAction.EventCompleted, Branch: "jonnii/20260724022739/make-restack-output-terse-and-outcome-focused", Message: "merged into main"}),
			syncEvent(syncAction.Event{Phase: syncAction.PhaseRestack, Type: syncAction.EventStarted}),
			restackEvent(actions.RestackBranchEvent{Result: actions.RestackDone, Branch: "info-query-cli-rendering", PRNumber: prNumberPointer(936), Parent: "main", NewRevision: "9e49378"}),
			restackEvent(actions.RestackBranchEvent{Result: actions.RestackDone, Branch: "jonnii/20260529015605/add-user-menu-to-header-and-extend-it-to-repo", PRNumber: prNumberPointer(1070), Parent: "main", NewRevision: "15d7955"}),
			restackEvent(actions.RestackBranchEvent{Result: actions.RestackDone, Branch: "jonnii/20260605031052/take-rename-s-prompt-off-the-TUI-drop-tui-from", PRNumber: prNumberPointer(1164), Parent: "main", NewRevision: "b0d42a4"}),
			restackEvent(actions.RestackBranchEvent{Result: actions.RestackDone, Branch: "jonnii/20260605032227/extract-OpenEditor-into-internal/editor-out-of", PRNumber: prNumberPointer(1242), Parent: "jonnii/20260605031052/take-rename-s-prompt-off-the-TUI-drop-tui-from", NewRevision: "e1d9f30"}),
			restackEvent(actions.RestackBranchEvent{Result: actions.RestackDone, Branch: "jonnii/20260605033020/route-insert-s-child-select-prompt-through-the", PRNumber: prNumberPointer(1241), Parent: "jonnii/20260605032227/extract-OpenEditor-into-internal/editor-out-of", NewRevision: "4cb45e0"}),
			restackEvent(actions.RestackBranchEvent{Result: actions.RestackUnneeded, Branch: "jonnii/20260219034124/prompt-notes-wt"}),
			restackEvent(actions.RestackBranchEvent{Result: actions.RestackConflict, Branch: "jonnii/20260220125253/add-prompt-notes-to-track-LLM-context-on-commits", PRNumber: prNumberPointer(754)}),
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
		Steps: []SyncStep{
			syncEvent(syncAction.Event{Phase: syncAction.PhaseTrunk, Type: syncAction.EventStarted}),
			syncEvent(syncAction.Event{Phase: syncAction.PhaseGitHub, Type: syncAction.EventStarted}),
			syncEvent(syncAction.Event{Phase: syncAction.PhaseTrunk, Type: syncAction.EventCompleted, Branch: "main"}),
			syncEvent(syncAction.Event{Phase: syncAction.PhaseBranches, Type: syncAction.EventStarted}),
			syncEvent(syncAction.Event{Phase: syncAction.PhaseBranches, Type: syncAction.EventSkipped, Branch: "feat/api", Conflict: true}),
			syncEvent(syncAction.Event{Phase: syncAction.PhaseRestack, Type: syncAction.EventStarted}),
			restackEvent(actions.RestackBranchEvent{Result: actions.RestackConflict, Branch: "feat/web", PRNumber: new(git.PRNumber(43))}),
		},
		Summary: syncAction.Summary{
			BranchesSkipped:  2,
			ConflictBranches: []string{"feat/web"},
		},
	},
	{
		Name:        "current",
		Description: "The lightweight no-op path where trunk is already current.",
		Steps: []SyncStep{
			syncEvent(syncAction.Event{Phase: syncAction.PhaseTrunk, Type: syncAction.EventStarted}),
			syncEvent(syncAction.Event{Phase: syncAction.PhaseGitHub, Type: syncAction.EventStarted}),
			syncEvent(syncAction.Event{Phase: syncAction.PhaseTrunk, Type: syncAction.EventCompleted, Branch: "main"}),
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
