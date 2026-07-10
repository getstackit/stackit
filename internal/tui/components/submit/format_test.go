package submit

import (
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

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

func TestFormatFinalList(t *testing.T) {
	t.Parallel()

	summary := FormatFinalList([]Item{
		{
			BranchName: "jonnii/20260511011552/guard-runner.repoRoot-reads-with-repoMu-to-fix",
			Action:     ActionUpdate,
			Status:     StatusDone,
			URL:        "https://github.com/getstackit/stackit/pull/934",
		},
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
			Error:      errors.New("remote rejected"),
		},
	})

	require.Equal(t,
		"\x1b]8;;https://github.com/getstackit/stackit/pull/934\x1b\\✓ guard-runner.repoRoot-reads-with-repoMu-to-fix #934 updated\x1b]8;;\x1b\\\n"+
			// Only the newly created PR prints its raw URL.
			"\x1b]8;;https://github.com/getstackit/stackit/pull/935\x1b\\✓ add-feature #935 created\x1b]8;;\x1b\\\n"+
			"     https://github.com/getstackit/stackit/pull/935\n"+
			"✗ fix-bug — remote rejected",
		summary)
	require.NotContains(t, summary, "Pull requests")
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
	require.Contains(t, summary, "✓ add-feature #935 created")
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

func TestFormatClosingSummary(t *testing.T) {
	t.Parallel()

	items := []Item{
		{BranchName: "a", Action: ActionCreate, Status: StatusDone},
		{BranchName: "b", Action: ActionUpdate, Status: StatusDone},
		{BranchName: "c", Action: ActionUpdate, Status: StatusDone},
		{BranchName: "d", Action: ActionUpdate, Status: StatusError, Error: errors.New("boom")},
	}

	require.Equal(t,
		"✗ 1 created, 2 updated, 1 failed, 4 unchanged (6.2s)",
		FormatClosingSummary(items, 4, 6200*time.Millisecond))
	require.Equal(t,
		"✓ 1 created (1m23s)",
		FormatClosingSummary(items[:1], 0, 83*time.Second))
	require.Equal(t, "✓ 1 created", FormatClosingSummary(items[:1], 0, 0))
	require.Empty(t, FormatClosingSummary(nil, 0, time.Second))
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
	require.Contains(t, summary, "✓ feature-1 #934 updated")
	require.NotContains(t, summary, "Submitting")
}

func stripANSIEscape(s string) string {
	return ansiPattern.ReplaceAllString(s, "")
}

var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;]*m`)
