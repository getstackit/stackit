package stack

import (
	"strings"
	"testing"

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
	handler.OnEvent(submitAction.CompletionEvent{Success: true, Message: "Submit complete"})

	got := out.String()
	require.Contains(t, got, "Submitting 1 branch")
	require.Contains(t, got, "✓ guard-runner.repoRoot-reads-with-repoMu-to-fix #934")
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
	handler.OnEvent(submitAction.CompletionEvent{Success: true, Message: "Submit complete"})

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

	got := out.String()
	require.Equal(t, 1, strings.Count(got, "Stack to submit:"))
	require.Contains(t, got, "● current-branch [CORE] → create")
	require.Contains(t, got, "skipped-branch")
	require.Contains(t, got, "— no changes")
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
	handler.OnEvent(submitAction.CompletionEvent{Success: true, Message: "All PRs up to date"})

	got := out.String()
	require.Contains(t, got, "Stack to submit:")
	require.Contains(t, got, "up-to-date-branch")
	require.Contains(t, got, "— no changes")
	require.Contains(t, got, "All PRs up to date")
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
	handler.OnEvent(submitAction.CompletionEvent{Success: true, Message: "All PRs up to date"})

	require.Contains(t, out.String(), "All PRs up to date")
}
