package stack

import (
	"time"

	submitAction "github.com/getstackit/stackit/internal/actions/submit"
	"github.com/getstackit/stackit/internal/engine"
)

// SubmitScenario is a deterministic replay of submit action events. It drives
// the production Handler, preserving the command's lazy TUI startup and its
// regular terminal output before and after the submission phase.
type SubmitScenario struct {
	Name        string
	Description string
	Events      []submitAction.Event
}

// Replay sends this scenario through a submit handler. delay makes the active
// submission states visible; zero is useful for automated checks.
func (s SubmitScenario) Replay(handler submitAction.Handler, delay time.Duration) {
	for _, event := range s.Events {
		handler.OnEvent(event)
		pause(delay)
	}
}

// SubmitScenarios are the named submit flows available in the local TUI lab.
var SubmitScenarios = []SubmitScenario{
	{
		Name:        "success",
		Description: "Update one PR, create another, and surface a non-fatal warning.",
		Events: []submitAction.Event{
			submitAction.StackDisplayEvent{Stack: submitAction.StackSnapshot{
				Branches:      []string{"feat/api", "feat/web"},
				CurrentBranch: "feat/web",
				TrunkBranch:   "main",
				ParentMap:     map[string]string{"feat/api": "main", "feat/web": "feat/api"},
				FixedMap:      map[string]bool{"feat/api": true, "feat/web": true},
				ScopeMap:      map[string]string{"feat/api": "API", "feat/web": "WEB"},
			}},
			submitAction.BranchPlanEvent{BranchName: "feat/api", Action: engine.SubmitActionUpdate, PRNumber: intPointer(42)},
			submitAction.BranchPlanEvent{BranchName: "feat/web", Action: engine.SubmitActionCreate, IsCurrent: true},
			submitAction.PlanningCompleteEvent{},
			submitAction.SubmissionStartEvent{Branches: []submitAction.BranchInfo{
				{Name: "feat/api", Action: engine.SubmitActionUpdate, PRNumber: intPointer(42)},
				{Name: "feat/web", Action: engine.SubmitActionCreate},
			}},
			submitAction.BranchProgressEvent{BranchName: "feat/api", Status: submitAction.StatusSubmitting},
			submitAction.BranchProgressEvent{BranchName: "feat/api", Status: submitAction.StatusDone, URL: "https://github.com/getstackit/stackit/pull/42"},
			submitAction.BranchProgressEvent{BranchName: "feat/web", Status: submitAction.StatusSubmitting},
			submitAction.BranchWarningEvent{BranchName: "feat/web", Warning: "could not apply reviewer @octo"},
			submitAction.BranchProgressEvent{BranchName: "feat/web", Status: submitAction.StatusDone, URL: "https://github.com/getstackit/stackit/pull/43"},
			submitAction.CompletionEvent{Outcome: submitAction.OutcomeComplete, Duration: 2 * time.Second},
		},
	},
	{
		Name:        "current",
		Description: "An all-current plan that never starts the submit TUI.",
		Events: []submitAction.Event{
			submitAction.StackDisplayEvent{Stack: submitAction.StackSnapshot{
				Branches:      []string{"feat/api"},
				CurrentBranch: "feat/api",
				TrunkBranch:   "main",
				ParentMap:     map[string]string{"feat/api": "main"},
				FixedMap:      map[string]bool{"feat/api": true},
			}},
			submitAction.BranchPlanEvent{BranchName: "feat/api", Skipped: true, SkipReason: "already up to date"},
			submitAction.PlanningCompleteEvent{},
			submitAction.CompletionEvent{Outcome: submitAction.OutcomeUpToDate, Message: "All pull requests are up to date"},
		},
	},
	{
		Name:        "ss",
		Description: "An st ss run that creates the current PR and retains its unchanged parent.",
		Events: []submitAction.Event{
			submitAction.StackDisplayEvent{Stack: submitAction.StackSnapshot{
				Branches:      []string{"add-sync-TUI-lab", "add-submit-TUI-lab-scenarios"},
				CurrentBranch: "add-submit-TUI-lab-scenarios",
				TrunkBranch:   "main",
				ParentMap: map[string]string{
					"add-sync-TUI-lab":             "main",
					"add-submit-TUI-lab-scenarios": "add-sync-TUI-lab",
				},
				FixedMap: map[string]bool{
					"add-sync-TUI-lab":             true,
					"add-submit-TUI-lab-scenarios": true,
				},
			}},
			submitAction.BranchPlanEvent{BranchName: "add-sync-TUI-lab", Skipped: true, SkipReason: "no changes"},
			submitAction.BranchPlanEvent{BranchName: "add-submit-TUI-lab-scenarios", Action: engine.SubmitActionCreate, IsCurrent: true},
			submitAction.PlanningCompleteEvent{},
			submitAction.SubmissionStartEvent{Branches: []submitAction.BranchInfo{
				{Name: "add-submit-TUI-lab-scenarios", Action: engine.SubmitActionCreate},
			}},
			submitAction.BranchProgressEvent{BranchName: "add-submit-TUI-lab-scenarios", Status: submitAction.StatusSubmitting},
			submitAction.BranchProgressEvent{BranchName: "add-submit-TUI-lab-scenarios", Status: submitAction.StatusDone, URL: "https://github.com/getstackit/stackit/pull/1417"},
			submitAction.CompletionEvent{Outcome: submitAction.OutcomeComplete, Duration: 5500 * time.Millisecond},
		},
	},
}

// LookupSubmitScenario returns a named submit scenario.
func LookupSubmitScenario(name string) (SubmitScenario, bool) {
	for _, scenario := range SubmitScenarios {
		if scenario.Name == name {
			return scenario, true
		}
	}
	return SubmitScenario{}, false
}

// SubmitScenarioNames returns scenario names in display order.
func SubmitScenarioNames() []string {
	names := make([]string, 0, len(SubmitScenarios))
	for _, scenario := range SubmitScenarios {
		names = append(names, scenario.Name)
	}
	return names
}
