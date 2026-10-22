package main

import (
	"fmt"
	"maps"
	"os"
	"slices"
	"time"

	"github.com/getstackit/stackit/internal/cli/stack"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/handlers"
	"github.com/getstackit/stackit/internal/output"
	"github.com/getstackit/stackit/internal/tui"
)

// RestackScenario drives the standalone handler, including validation activity.
type RestackScenario struct {
	Events  []handlers.RestackBranchEvent
	Summary handlers.RestackSummary
}

var restackScenarios = map[string]RestackScenario{
	"success": {
		Events: []handlers.RestackBranchEvent{
			{Branch: "feat/api", Parent: "main", Result: handlers.RestackDone, NewRevision: "a1b2c3d"},
			{Branch: "feat/web", Parent: "feat/api", Result: handlers.RestackDone, NewRevision: "d4e5f6a", IsCurrent: true},
			{Branch: "feat/docs", Result: handlers.RestackUnneeded},
		},
		Summary: handlers.RestackSummary{Restacked: 2},
	},
	"current": {
		Events: []handlers.RestackBranchEvent{{Branch: "feat/api", Result: handlers.RestackUnneeded}},
	},
	"held": {
		Events:  []handlers.RestackBranchEvent{{Branch: "feat/api", Result: handlers.RestackUnneeded, HeldBy: "worktree /tmp/api has uncommitted changes"}},
		Summary: handlers.RestackSummary{Held: []handlers.RestackHeldInfo{{Branch: "feat/api", Reason: "worktree /tmp/api has uncommitted changes"}}},
	},
	"conflict": {
		Events: []handlers.RestackBranchEvent{
			{Branch: "feat/api", Parent: "main", Result: handlers.RestackBlocked},
			{Branch: "feat/web", Parent: "feat/api", Result: handlers.RestackConflict},
		},
		Summary: handlers.RestackSummary{Skipped: 1, Conflicts: []string{"feat/web"}, Blocked: []string{"feat/api"}},
	},
}

func (s RestackScenario) Replay(handler handlers.RestackHandler, delay time.Duration) {
	handler.OnRestackStart(len(s.Events))
	activity := handlers.RestackActivity(handler)
	for _, event := range s.Events {
		if event.Result == handlers.RestackDone && activity != nil {
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
	if tui.IsTTY() {
		out.Info("%s", stack.RestackHeadline(stack.RestackScope{Target: "feat/api", Trunk: "main"}, len(scenario.Events)))
	}
	runner, handler := stack.NewSyncUI(out, output.NewNullLogger())
	defer runner.Cleanup()
	scenario.Replay(handler, delay)
}
