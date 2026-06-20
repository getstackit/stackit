package stack

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"

	submitAction "github.com/getstackit/stackit/internal/actions/submit"
	"github.com/getstackit/stackit/internal/output"
	submitComponent "github.com/getstackit/stackit/internal/tui/components/submit"
)

func TestSimpleSubmitHandlerPrintsFinalURLSummary(t *testing.T) {
	t.Parallel()

	out := output.NewTestOutput()
	handler := NewSimpleSubmitHandler(out)
	branch := "jonnii/20260511011552/guard-runner.repoRoot-reads-with-repoMu-to-fix"

	handler.OnEvent(submitAction.SubmissionStartEvent{
		Branches: []submitAction.BranchInfo{{
			Name:   branch,
			Action: "update",
		}},
	})
	handler.OnEvent(submitAction.BranchProgressEvent{
		BranchName: branch,
		Status:     submitAction.StatusDone,
		URL:        "https://github.com/getstackit/stackit/pull/934",
	})
	handler.OnEvent(submitAction.CompletionEvent{Outcome: submitAction.OutcomeComplete, Message: "Submit complete"})

	got := out.String()
	require.Contains(t, got, "Submitting 1 branch")
	require.Contains(t, got, "✓ guard-runner.repoRoot-reads-with-repoMu-to-fix #934 updated")
	require.Contains(t, got, "Pull requests")
	require.Contains(t, got, "#934 guard-runner.repoRoot-reads-with-repoMu-to-fix")
	require.Contains(t, got, "     https://github.com/getstackit/stackit/pull/934")
	require.NotContains(t, got, "updated → https://github.com/getstackit/stackit/pull/934")
}

func TestSimpleSubmitHandlerPreservesURLAcrossFooterSync(t *testing.T) {
	t.Parallel()

	out := output.NewTestOutput()
	handler := NewSimpleSubmitHandler(out)
	branch := "jonnii/20260511011552/guard-runner.repoRoot-reads-with-repoMu-to-fix"

	handler.OnEvent(submitAction.SubmissionStartEvent{
		Branches: []submitAction.BranchInfo{{
			Name:   branch,
			Action: "update",
		}},
	})
	handler.OnEvent(submitAction.BranchProgressEvent{
		BranchName: branch,
		Status:     submitAction.StatusDone,
		URL:        "https://github.com/getstackit/stackit/pull/934",
	})
	handler.OnEvent(submitAction.BranchProgressEvent{
		BranchName: branch,
		Status:     submitAction.StatusSyncing,
	})
	handler.OnEvent(submitAction.BranchProgressEvent{
		BranchName: branch,
		Status:     submitAction.StatusDone,
	})
	handler.OnEvent(submitAction.CompletionEvent{Outcome: submitAction.OutcomeComplete, Message: "Submit complete"})

	got := out.String()
	require.Contains(t, got, "https://github.com/getstackit/stackit/pull/934")
	require.NotContains(t, got, "syncing")
	require.Equal(t, 1, strings.Count(got, "✓"), "footer sync must not re-report a finished branch")
}

func TestSimpleSubmitHandlerMergesPlanIntoStackList(t *testing.T) {
	t.Parallel()

	out := output.NewTestOutput()
	handler := NewSimpleSubmitHandler(out)
	current := "jonnii/20260511011552/current-branch"
	skipped := "jonnii/20260511011552/skipped-branch"

	handler.OnEvent(submitAction.StackDisplayEvent{Stack: submitAction.StackSnapshot{
		Branches:      []string{skipped, current},
		CurrentBranch: current,
		TrunkBranch:   "main",
		ScopeMap:      map[string]string{current: "CORE"},
	}})
	handler.OnEvent(submitAction.BranchPlanEvent{
		BranchName: skipped,
		Skipped:    true,
		SkipReason: "no changes",
	})
	handler.OnEvent(submitAction.BranchPlanEvent{
		BranchName: current,
		Action:     "create",
		IsCurrent:  true,
	})
	handler.OnEvent(submitAction.PlanningCompleteEvent{})

	got := ansi.Strip(out.String())
	require.Contains(t, got, "Submit plan → main")
	require.Contains(t, got, "Will submit (1)")
	require.Contains(t, got, "● current-branch [CORE] → create")
	require.Contains(t, got, "No changes (1)")
	require.NotContains(t, got, "skipped-branch")
	require.NotContains(t, got, "skipped-branch — no changes")
	require.Equal(t, 1, strings.Count(got, "current-branch"), "stack and plan must print as one merged list")
}

func TestInteractiveSubmitHandlerPrintsPlanWithoutStartingTUI(t *testing.T) {
	t.Parallel()

	out := output.NewTestOutput()
	// A nil runner stands in for a TUI that was never started; every runner
	// method is nil-safe. Plan output must not depend on the TUI running.
	handler := NewInteractiveSubmitHandler(nil, submitComponent.NewModel(nil), out)
	branch := "jonnii/20260511011552/up-to-date-branch"

	handler.OnEvent(submitAction.StackDisplayEvent{Stack: submitAction.StackSnapshot{
		Branches:      []string{branch},
		CurrentBranch: branch,
		TrunkBranch:   "main",
	}})
	handler.OnEvent(submitAction.BranchPlanEvent{
		BranchName: branch,
		Skipped:    true,
		SkipReason: "no changes",
		IsCurrent:  true,
	})
	handler.OnEvent(submitAction.CompletionEvent{Outcome: submitAction.OutcomeUpToDate, Message: "All PRs up to date"})

	got := ansi.Strip(out.String())
	// A lone branch drops the stack framing: no "Submit plan" header, the
	// branch reads as "name → base".
	require.NotContains(t, got, "Submit plan")
	require.Contains(t, got, "up-to-date-branch → main")
	require.Contains(t, got, "— no changes")
	require.Contains(t, got, "All PRs up to date")
}

func TestPlanPrinterShowsPRNumbersAndEmptyAnnotation(t *testing.T) {
	t.Parallel()

	out := output.NewTestOutput()
	handler := NewSimpleSubmitHandler(out)
	prNumber := 1189

	handler.OnEvent(submitAction.StackDisplayEvent{Stack: submitAction.StackSnapshot{
		Branches:    []string{"update-me", "empty-one"},
		TrunkBranch: "main",
	}})
	handler.OnEvent(submitAction.BranchPlanEvent{
		BranchName: "update-me",
		Action:     "update",
		PRNumber:   &prNumber,
	})
	handler.OnEvent(submitAction.BranchPlanEvent{
		BranchName: "empty-one",
		Action:     "create",
		Empty:      true,
	})
	handler.OnEvent(submitAction.PlanningCompleteEvent{})

	got := ansi.Strip(out.String())
	require.Contains(t, got, "Submit plan → main")
	require.Contains(t, got, "Will submit (2)")
	require.Contains(t, got, "update-me → update #1189")
	require.Contains(t, got, "empty-one → create")
	require.Contains(t, got, "(empty)")
}

func TestSimpleSubmitHandlerSoloSubmitDropsStackFraming(t *testing.T) {
	t.Parallel()

	out := output.NewTestOutput()
	handler := NewSimpleSubmitHandler(out)
	branch := "jonnii/20260511011552/tighten-submit-output"

	handler.OnEvent(submitAction.StackDisplayEvent{Stack: submitAction.StackSnapshot{
		Branches:      []string{branch},
		CurrentBranch: branch,
		TrunkBranch:   "main",
		ParentMap:     map[string]string{branch: "main"},
	}})
	handler.OnEvent(submitAction.BranchPlanEvent{
		BranchName: branch,
		Action:     "create",
		IsCurrent:  true,
	})
	handler.OnEvent(submitAction.SubmissionStartEvent{
		Branches: []submitAction.BranchInfo{{Name: branch, Action: "create"}},
	})
	handler.OnEvent(submitAction.BranchProgressEvent{
		BranchName: branch,
		Status:     submitAction.StatusDone,
		URL:        "https://github.com/getstackit/stackit/pull/1270",
	})
	handler.OnEvent(submitAction.CompletionEvent{Outcome: submitAction.OutcomeComplete, Message: "Submit complete"})

	got := ansi.Strip(out.String())
	require.Contains(t, got, "tighten-submit-output → main")
	require.NotContains(t, got, "Submit plan")
	require.NotContains(t, got, "Submitting 1 branch")
	require.NotContains(t, got, "Pull requests")
	require.Contains(t, got, "#1270 created")
	require.Contains(t, got, "https://github.com/getstackit/stackit/pull/1270")
}

func TestSimpleSubmitHandlerOnTrunkGuidance(t *testing.T) {
	t.Parallel()

	out := output.NewTestOutput()
	handler := NewSimpleSubmitHandler(out)

	handler.OnEvent(submitAction.CompletionEvent{
		Outcome: submitAction.OutcomeOnTrunk,
		Message: "You're on main — nothing to submit from here.",
	})

	got := out.String()
	require.Contains(t, got, "You're on main — nothing to submit from here.")
	require.Contains(t, got, "stackit checkout <branch>")
	require.Contains(t, got, "stackit create")
}

func TestSimpleSubmitHandlerPrintsBranchWarnings(t *testing.T) {
	t.Parallel()

	out := output.NewTestOutput()
	handler := NewSimpleSubmitHandler(out)

	handler.OnEvent(submitAction.BranchWarningEvent{
		BranchName: "jonnii/20260511011552/add-feature",
		Warning:    "failed to add labels: not found",
	})

	require.Contains(t, out.String(), "add-feature: failed to add labels: not found")
}

func TestSimpleSubmitHandlerStaysQuietOnFailureOutcome(t *testing.T) {
	t.Parallel()

	out := output.NewTestOutput()
	handler := NewSimpleSubmitHandler(out)

	// The CLI prints the returned error; the handler must not double-report.
	handler.OnEvent(submitAction.CompletionEvent{Outcome: submitAction.OutcomeFailed, Message: "Submit failed"})

	require.NotContains(t, out.String(), "Submit failed")
}

func TestSimpleSubmitHandlerPrintsOutcomeWhenNothingSubmitted(t *testing.T) {
	t.Parallel()

	out := output.NewTestOutput()
	handler := NewSimpleSubmitHandler(out)
	branch := "jonnii/20260511011552/up-to-date-branch"

	handler.OnEvent(submitAction.StackDisplayEvent{Stack: submitAction.StackSnapshot{
		Branches:      []string{branch},
		CurrentBranch: branch,
		TrunkBranch:   "main",
	}})
	handler.OnEvent(submitAction.BranchPlanEvent{
		BranchName: branch,
		Skipped:    true,
		SkipReason: "no changes",
	})
	handler.OnEvent(submitAction.CompletionEvent{Outcome: submitAction.OutcomeUpToDate, Message: "All PRs up to date"})

	require.Contains(t, out.String(), "All PRs up to date")
}
