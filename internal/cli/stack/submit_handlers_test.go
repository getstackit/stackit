package stack

import (
	"testing"

	"github.com/stretchr/testify/require"

	submitAction "github.com/getstackit/stackit/internal/actions/submit"
	"github.com/getstackit/stackit/internal/output"
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

	require.Contains(t, out.String(), "https://github.com/getstackit/stackit/pull/934")
}
