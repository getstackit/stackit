package submit

import (
	"errors"
	"regexp"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
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
	require.Contains(t, row, "#934 updated")
	require.NotContains(t, row, "https://github.com/getstackit/stackit/pull/934")
	require.LessOrEqual(t, lipgloss.Width(stripANSIEscape(row)), 60)
}

func TestFormatSoloRowOmitsBranchName(t *testing.T) {
	t.Parallel()

	pr := 1270
	row := stripANSIEscape(FormatSoloRow(Item{
		BranchName: "jonnii/20260511011552/tighten-submit-output",
		Action:     ActionCreate,
		Status:     StatusDone,
		PRNumber:   &pr,
		URL:        "https://github.com/getstackit/stackit/pull/1270",
	}, "", DefaultStyles()))

	require.Equal(t, "  ✓ #1270 created", row)
	require.NotContains(t, row, "tighten-submit-output")
}

func TestFormatSoloSummaryShowsRefAndURL(t *testing.T) {
	t.Parallel()

	pr := 1270
	summary := FormatSoloSummary([]Item{{
		BranchName: "jonnii/20260511011552/tighten-submit-output",
		Action:     ActionCreate,
		Status:     StatusDone,
		PRNumber:   &pr,
		URL:        "https://github.com/getstackit/stackit/pull/1270",
	}})

	require.Equal(t, "  #1270 created\n  https://github.com/getstackit/stackit/pull/1270", summary)
	require.NotContains(t, summary, "Pull requests")
}

func TestModelSoloDropsHeaderAndName(t *testing.T) {
	t.Parallel()

	pr := 1270
	m := NewModel([]Item{{
		BranchName: "jonnii/20260511011552/tighten-submit-output",
		Action:     ActionCreate,
		Status:     StatusDone,
		PRNumber:   &pr,
		URL:        "https://github.com/getstackit/stackit/pull/1270",
	}})
	m.Solo = true

	content := stripANSIEscape(m.content())
	require.NotContains(t, content, "Submitting")
	require.NotContains(t, content, "tighten-submit-output")
	require.Contains(t, content, "#1270 created")

	summary := stripANSIEscape(m.completionSummary())
	require.NotContains(t, summary, "Pull requests")
	require.Contains(t, summary, "https://github.com/getstackit/stackit/pull/1270")
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

func TestFormatLinkedURLSummaryEmitsHyperlinks(t *testing.T) {
	t.Parallel()

	summary := FormatLinkedURLSummary([]Item{
		{
			BranchName: "jonnii/20260511011552/add-feature",
			Action:     ActionCreate,
			Status:     StatusDone,
			URL:        "https://github.com/getstackit/stackit/pull/935",
		},
	})

	require.Equal(t, "Pull requests\n\n"+
		"\x1b]8;;https://github.com/getstackit/stackit/pull/935\x1b\\#935 add-feature\x1b]8;;\x1b\\\n"+
		"     https://github.com/getstackit/stackit/pull/935",
		summary)
}

func TestModelCompletionSummaryIncludesFailuresAndWarnings(t *testing.T) {
	t.Parallel()

	m := NewModel([]Item{
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
	updated, _ := m.Update(WarningMsg{
		BranchName: "jonnii/20260511011552/add-feature",
		Warning:    "failed to add labels",
	})
	m = updated.(*Model)

	summary := m.completionSummary()
	require.Contains(t, summary, "Pull requests")
	require.Contains(t, summary, "#935 add-feature")
	require.Contains(t, summary, "✗ fix-bug — failed to push branch: remote rejected")
	require.Contains(t, summary, "⚠️  add-feature: failed to add labels")
}

func TestFormatCompactRowTruncatesLongErrors(t *testing.T) {
	t.Parallel()

	long := "failed to push branch: " + strings.Repeat("x", 100) + "\nhint: check your remote\nhint: and your credentials"
	row := ansi.Strip(FormatCompactRow(Item{
		BranchName: "jonnii/20260511011552/fix-bug",
		Action:     ActionUpdate,
		Status:     StatusError,
		Error:      errors.New(long),
	}, 80, "", DefaultStyles()))

	require.NotContains(t, row, "\n", "row must stay on one line")
	require.NotContains(t, row, "hint:")
	require.Contains(t, row, "...")
	require.LessOrEqual(t, lipgloss.Width(row), 80+maxErrorDetailWidth, "detail must be capped")
}

func TestFormatFailureSummaryWithoutErrorDetail(t *testing.T) {
	t.Parallel()

	summary := FormatFailureSummary([]Item{
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

func TestModelCompletionSummaryFallsBackToPlanView(t *testing.T) {
	t.Parallel()

	m := NewModel([]Item{
		{BranchName: "feature-1", Action: ActionUpdate, Status: StatusPending, IsSkipped: true, SkipReason: "no changes"},
		{BranchName: "feature-2", Action: ActionCreate, Status: StatusPending},
	})

	summary := stripANSIEscape(m.completionSummary())
	require.Contains(t, summary, "Submitting 2 branches")
	require.Contains(t, summary, "feature-1")
	require.Contains(t, summary, "no changes")
	require.Contains(t, summary, "feature-2")
	require.Contains(t, summary, "create")
}

func TestModelCompletionSummaryPrefersSubmissionResults(t *testing.T) {
	t.Parallel()

	m := NewModel([]Item{{
		BranchName: "feature-1",
		Action:     ActionUpdate,
		Status:     StatusDone,
		URL:        "https://github.com/getstackit/stackit/pull/934",
	}})

	summary := m.completionSummary()
	require.Contains(t, summary, "Pull requests")
	require.Contains(t, summary, "https://github.com/getstackit/stackit/pull/934")
}

func stripANSIEscape(s string) string {
	return ansiPattern.ReplaceAllString(s, "")
}

var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;]*m`)
