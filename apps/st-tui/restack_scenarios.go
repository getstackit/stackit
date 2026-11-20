package main

import (
	"fmt"
	"maps"
	"os"
	"slices"
	"time"

	"github.com/getstackit/stackit/internal/actions"
	"github.com/getstackit/stackit/internal/cli/stack"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/output"
	"github.com/getstackit/stackit/internal/term"
)

// RestackScenario drives the standalone handler, including validation activity.
type RestackScenario struct {
	Events  []actions.RestackBranchEvent
	Summary actions.RestackSummary
}

var restackScenarios = map[string]RestackScenario{
	"success": {
		Events: []actions.RestackBranchEvent{
			{Branch: "feat/api", Parent: "main", Result: actions.RestackDone, NewRevision: "a1b2c3d"},
			{Branch: "feat/web", Parent: "feat/api", Result: actions.RestackDone, NewRevision: "d4e5f6a", IsCurrent: true},
			{Branch: "feat/docs", Result: actions.RestackUnneeded},
		},
		Summary: actions.RestackSummary{Restacked: 2},
	},
	"current": {
		Events: []actions.RestackBranchEvent{{Branch: "feat/api", Result: actions.RestackUnneeded}},
	},
	"held": {
		Events:  []actions.RestackBranchEvent{{Branch: "feat/api", Result: actions.RestackUnneeded, HeldBy: "worktree /tmp/api has uncommitted changes"}},
		Summary: actions.RestackSummary{Held: []actions.RestackHeldInfo{{Branch: "feat/api", Reason: "worktree /tmp/api has uncommitted changes"}}},
	},
	"conflict": {
		Events: []actions.RestackBranchEvent{
			{Branch: "feat/api", Parent: "main", Result: actions.RestackBlocked},
			{Branch: "feat/web", Parent: "feat/api", Result: actions.RestackConflict},
		},
		Summary: actions.RestackSummary{Conflicts: []string{"feat/web"}, Blocked: []string{"feat/api"}},
	},
}

func (s RestackScenario) Replay(handler actions.RestackHandler, delay time.Duration) {
	handler.OnRestackStart(len(s.Events))
	activity := actions.RestackActivity(handler)
	for _, event := range s.Events {
		if event.Result == actions.RestackDone && activity != nil {
			activity(engine.RebaseProgress{Branch: event.Branch, Parent: event.Parent})
			pause(delay)
			activity(engine.RebaseProgress{Branch: event.Branch, Parent: event.Parent, Finished: true})
		}
		handler.OnRestackBranch(event)
		pause(delay)
	}
	handler.OnRestackComplete(s.Summary)
}

// RestackScenarioNames returns the restack scenario names in sorted order.
func RestackScenarioNames() []string {
	return slices.Sorted(maps.Keys(restackScenarios))
}

func runRestackScenario(out output.Output, name string, delay time.Duration) {
	scenario, found := restackScenarios[name]
	if !found {
		_, _ = fmt.Fprintf(os.Stderr, "unknown restack scenario %q\n\n", name)
		printUsage(os.Stderr)
		os.Exit(2)
	}
	if name == "current" {
		// Match the real command's no-op path without starting Bubble Tea.
		scenario.Replay(stack.NewSimpleSyncHandler(out), delay)
		return
	}
	// Replays run from a feature branch with no scope flags, like a plain
	// `stackit restack`, so they share the real command's headline — which,
	// like production, only prints when the interactive TUI runs.
	if term.IsInteractive() {
		out.Info("%s", stack.RestackHeadline(stack.RestackScope{Target: "feat/api", Trunk: "main"}, len(scenario.Events)))
	}
	runner, handler := stack.NewSyncUI(out, output.NewNullLogger())
	defer runner.Cleanup()
	scenario.Replay(handler, delay)
}
