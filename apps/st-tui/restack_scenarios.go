package main

import (
	"fmt"
	"time"

	"github.com/getstackit/stackit/internal/cli/stack"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/handlers"
	"github.com/getstackit/stackit/internal/output"
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
		Events: []handlers.RestackBranchEvent{{Branch: "feat/api", Result: handlers.RestackUnneeded, HeldBy: "worktree /tmp/api has uncommitted changes"}},
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

func runRestackScenario(out output.Output, name string, delay time.Duration) error {
	scenario, found := restackScenarios[name]
	if !found {
		return fmt.Errorf("unknown restack scenario %q", name)
	}
	if name == "current" {
		// Match the real command's no-op path without starting Bubble Tea.
		scenario.Replay(stack.NewSimpleSyncHandler(out), delay)
		return nil
	}
	unit := "branches"
	if len(scenario.Events) == 1 {
		unit = "branch"
	}
	out.Info("Restacking current stack · %d %s", len(scenario.Events), unit)
	runner, handler := stack.NewSyncUI(out, output.NewNullLogger())
	defer runner.Cleanup()
	scenario.Replay(handler, delay)
	return nil
}
