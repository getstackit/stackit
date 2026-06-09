package submit

import (
	"errors"
	"regexp"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/stretchr/testify/require"
)

func TestDisplayBranchNameStripsStackitPrefix(t *testing.T) {
	t.Parallel()

	require.Equal(t,
		"guard-runner.repoRoot-reads-with-repoMu-to-fix",
		DisplayBranchName("jonnii/20260511011552/guard-runner.repoRoot-reads-with-repoMu-to-fix"))
	require.Equal(t,
		"refactor-unify-editor-command-launching",
		DisplayBranchName("refactor-unify-editor-command-launching"))
}

func TestFormatCompactRowOmitsInlineURL(t *testing.T) {
	t.Parallel()

	row := FormatCompactRow(Item{
		BranchName: "jonnii/20260511011552/guard-runner.repoRoot-reads-with-repoMu-to-fix",
		Action:     ActionUpdate,
		Status:     StatusDone,
		URL:        "https://github.com/getstackit/stackit/pull/934",
	}, 60, "", DefaultStyles())

	require.Contains(t, row, "guard-runner")
	require.Contains(t, row, "#934")
	require.NotContains(t, row, "https://github.com/getstackit/stackit/pull/934")
	require.LessOrEqual(t, lipgloss.Width(stripANSIEscape(row)), 60)
}

func TestFormatURLSummaryRendersClickableRows(t *testing.T) {
	t.Parallel()

	summary := FormatURLSummary([]Item{
		{
			BranchName: "jonnii/20260511011552/guard-runner.repoRoot-reads-with-repoMu-to-fix",
			Action:     ActionUpdate,
			Status:     StatusDone,
			URL:        "https://github.com/getstackit/stackit/pull/934",
		},
	})

	require.Equal(t, `Pull requests

#934 guard-runner.repoRoot-reads-with-repoMu-to-fix
     https://github.com/getstackit/stackit/pull/934`, summary)
}

func TestFormatCompletionSummaryIncludesFailures(t *testing.T) {
	t.Parallel()

	summary := FormatCompletionSummary([]Item{
		{
			BranchName: "jonnii/20260511011552/add-feature",
			Action:     ActionCreate,
			Status:     StatusDone,
			URL:        "https://github.com/getstackit/stackit/pull/935",
		},
		{
			BranchName: "jonnii/20260511011552/fix-bug",
			Action:     ActionUpdate,
			Status:     StatusError,
			Error:      errors.New("failed to push branch: remote rejected"),
		},
	})

	require.Equal(t, `Pull requests

#935 add-feature
     https://github.com/getstackit/stackit/pull/935

✗ fix-bug — failed to push branch: remote rejected`, summary)
}

func TestFormatCompletionSummaryFailuresWithoutURLs(t *testing.T) {
	t.Parallel()

	summary := FormatCompletionSummary([]Item{
		{
			BranchName: "jonnii/20260511011552/fix-bug",
			Action:     ActionUpdate,
			Status:     StatusError,
		},
	})

	require.Equal(t, "✗ fix-bug — failed", summary)
}

func TestModelPreservesURLAcrossFooterSyncDone(t *testing.T) {
	t.Parallel()

	branch := "jonnii/20260511011552/guard-runner.repoRoot-reads-with-repoMu-to-fix"
	m := NewModel([]Item{{
		BranchName: branch,
		Action:     ActionUpdate,
		Status:     StatusPending,
	}})

	updated, _ := m.Update(ProgressUpdateMsg{
		BranchName: branch,
		Status:     StatusDone,
		URL:        "https://github.com/getstackit/stackit/pull/934",
	})
	m = updated.(*Model)
	updated, _ = m.Update(ProgressUpdateMsg{BranchName: branch, Status: StatusSyncing})
	m = updated.(*Model)
	updated, _ = m.Update(ProgressUpdateMsg{BranchName: branch, Status: StatusDone})
	m = updated.(*Model)

	require.Equal(t, "https://github.com/getstackit/stackit/pull/934", m.Items[0].URL)
}

func stripANSIEscape(s string) string {
	return ansiPattern.ReplaceAllString(s, "")
}

var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;]*m`)
