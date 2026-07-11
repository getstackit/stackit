package submit

import (
	"fmt"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"
	"unicode"

	"charm.land/lipgloss/v2"

	"github.com/getstackit/stackit/internal/engine"
)

const defaultSubmitWidth = 80

// DisplayBranchName returns a compact branch name for submit output.
func DisplayBranchName(branchName string) string {
	parts := strings.Split(branchName, "/")
	if len(parts) >= 3 && isTimestampSegment(parts[1]) {
		return strings.Join(parts[2:], "/")
	}
	return branchName
}

func isTimestampSegment(s string) bool {
	if len(s) < 12 {
		return false
	}
	for _, r := range s {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

// TruncateMiddle shortens s to maxWidth display cells, preserving both ends.
func TruncateMiddle(s string, maxWidth int) string {
	if maxWidth <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= maxWidth {
		return s
	}
	if maxWidth <= 3 {
		return truncateRunes(s, maxWidth)
	}

	ellipsis := "..."
	available := maxWidth - len(ellipsis)
	leftWidth := (available + 1) / 2
	rightWidth := available / 2

	left := takeRunes(s, leftWidth)
	right := takeLastRunes(s, rightWidth)
	return left + ellipsis + right
}

func truncateRunes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n])
}

func takeRunes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n])
}

func takeLastRunes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[len(runes)-n:])
}

// FormatCompactRow renders a single submit progress row.
func FormatCompactRow(item Item, width int, spinnerView string, styles Styles) string {
	if width <= 0 {
		width = defaultSubmitWidth
	}

	icon, detail := rowParts(item, spinnerView, styles)
	fixedWidth := lipgloss.Width("  "+icon+" ") + lipgloss.Width(detail) + 1
	nameWidth := max(1, width-fixedWidth)
	plainName := TruncateMiddle(DisplayBranchName(item.BranchName), nameWidth)
	name := styles.BranchStyle.Render(plainName)

	if detail == "" {
		return fmt.Sprintf("  %s %s", icon, name)
	}
	prefixWidth := lipgloss.Width("  "+icon+" ") + lipgloss.Width(plainName)
	gapWidth := max(1, width-prefixWidth-lipgloss.Width(detail))
	return fmt.Sprintf("  %s %s%s%s", icon, name, strings.Repeat(" ", gapWidth), detail)
}

// FormatSoloRow renders the progress row for a single-branch submit. The branch
// name is omitted — the plan line already named it — so the row is just the
// status icon and detail (e.g. "  ✓ #1270 created").
func FormatSoloRow(item Item, spinnerView string, styles Styles) string {
	icon, detail := rowParts(item, spinnerView, styles)
	if detail == "" {
		return "  " + icon
	}
	return "  " + icon + " " + detail
}

func rowParts(item Item, spinnerView string, styles Styles) (string, string) {
	switch item.Status {
	case StatusSubmitting:
		detail := "creating"
		if item.Action == ActionUpdate {
			detail = "updating"
		}
		return spinnerView, styles.SpinnerStyle.Render(detail)
	case StatusSyncing:
		return spinnerView, styles.SpinnerStyle.Render("syncing")
	case StatusDone:
		detail := pastTense(item.Action)
		if ref := PRRef(item); ref != "" {
			detail = ref + " " + detail
		}
		return styles.DoneStyle.Render("✓"), styles.DoneStyle.Render(detail)
	case StatusError:
		return styles.ErrorStyle.Render("✗"), styles.ErrorStyle.Render(compactErrorText(item.Error))
	case StatusPending, "":
		if item.IsSkipped {
			reason := item.SkipReason
			if reason == "" {
				reason = "skipped"
			}
			return styles.DimStyle.Render("○"), styles.DimStyle.Render(reason)
		}
		detail := actionLabel(item.Action)
		if detail == "" {
			detail = "pending"
		}
		return styles.DimStyle.Render("○"), styles.DimStyle.Render(detail)
	default:
		return styles.DimStyle.Render("○"), styles.DimStyle.Render(string(item.Status))
	}
}

// maxErrorDetailWidth caps the error text shown in a progress row so a long
// git/gh message cannot shred the column layout mid-TUI.
const maxErrorDetailWidth = 60

// errorText is err's message, or a generic label when there is none.
func errorText(err error) string {
	if err == nil {
		return "failed"
	}
	return err.Error()
}

// compactErrorText flattens an error to its trimmed first line, truncated to
// fit a progress row. The full text still persists via FormatFailureSummary
// when the TUI exits, so nothing is lost.
func compactErrorText(err error) string {
	detail := errorText(err)
	if i := strings.IndexByte(detail, '\n'); i >= 0 {
		detail = detail[:i]
	}
	detail = strings.TrimSpace(detail)
	if detail == "" {
		detail = errorText(nil)
	}
	return TruncateMiddle(detail, maxErrorDetailWidth)
}

func actionLabel(action engine.SubmitAction) string {
	switch action {
	case ActionCreate:
		return "create"
	case ActionUpdate:
		return "update"
	default:
		return string(action)
	}
}

func pastTense(action engine.SubmitAction) string {
	switch action {
	case ActionCreate:
		return "created"
	case ActionUpdate:
		return "updated"
	default:
		return "done"
	}
}

// PRRef returns a display PR number, deriving it from URL when necessary.
func PRRef(item Item) string {
	if item.PRNumber != nil {
		return fmt.Sprintf("#%d", *item.PRNumber)
	}
	if n, ok := prNumberFromURL(item.URL); ok {
		return fmt.Sprintf("#%d", n)
	}
	return ""
}

func prNumberFromURL(raw string) (int, bool) {
	if raw == "" {
		return 0, false
	}
	u, err := url.Parse(raw)
	if err != nil {
		return 0, false
	}
	base := path.Base(u.Path)
	if base == "." || base == "/" {
		return 0, false
	}
	n, err := strconv.Atoi(base)
	return n, err == nil
}

// hyperlink wraps text in an OSC 8 terminal hyperlink escape sequence.
// Terminals without OSC 8 support ignore the sequence and show the plain text.
func hyperlink(url, text string) string {
	return "\x1b]8;;" + url + "\x1b\\" + text + "\x1b]8;;\x1b\\"
}

// FormatFinalList renders the persisted post-submit result: one row per
// branch with its final status, the row hyperlinked to its PR. Raw URL lines
// print only for newly created PRs — those are the ones the user needs to
// open; updated PRs rarely need their URL re-pasted.
func FormatFinalList(items []Item) string {
	rows := make([]string, 0, len(items))
	for _, item := range items {
		name := DisplayBranchName(item.BranchName)
		switch item.Status {
		case StatusError:
			rows = append(rows, fmt.Sprintf("✗ %s — %s", name, errorText(item.Error)))

		case StatusDone:
			detail := pastTense(item.Action)
			if ref := PRRef(item); ref != "" {
				detail = ref + " " + detail
			}
			row := fmt.Sprintf("✓ %s %s", name, detail)
			if item.URL != "" {
				row = hyperlink(item.URL, row)
			}
			if item.Action == ActionCreate && item.URL != "" {
				row += "\n     " + item.URL
			}
			rows = append(rows, row)

		default:
			// Not a terminal state: nothing was submitted for this branch, so
			// there is no result to persist. An all-pending list stays empty,
			// which lets completionSummary fall back to the plan view.
		}
	}
	return strings.Join(rows, "\n")
}

// FormatSoloSummary renders the post-submit result for a single-branch submit:
// the PR ref and action on one line, the URL on the next, both indented. Unlike
// the stack summary there is no "Pull requests" header — one PR needs no list.
func FormatSoloSummary(items []Item) string {
	for _, item := range items {
		if item.URL == "" {
			continue
		}
		label := strings.TrimSpace(PRRef(item) + " " + pastTense(item.Action))
		return "  " + label + "\n  " + item.URL
	}
	return ""
}

// FormatClosingSummary renders the one-line run summary: counts by outcome
// plus elapsed time, e.g. "✓ 1 created, 2 updated, 4 unchanged (6.2s)".
func FormatClosingSummary(items []Item, skipped int, elapsed time.Duration) string {
	var created, updated, failed int
	for _, item := range items {
		switch {
		case item.Status == StatusError:
			failed++
		case item.Status == StatusDone && item.Action == ActionCreate:
			created++
		case item.Status == StatusDone && item.Action == ActionUpdate:
			updated++
		}
	}

	parts := make([]string, 0, 4)
	if created > 0 {
		parts = append(parts, fmt.Sprintf("%d created", created))
	}
	if updated > 0 {
		parts = append(parts, fmt.Sprintf("%d updated", updated))
	}
	if failed > 0 {
		parts = append(parts, fmt.Sprintf("%d failed", failed))
	}
	if skipped > 0 {
		parts = append(parts, fmt.Sprintf("%d unchanged", skipped))
	}
	if len(parts) == 0 {
		return ""
	}

	icon := "✓"
	if failed > 0 {
		icon = "✗"
	}
	line := icon + " " + strings.Join(parts, ", ")
	if elapsed > 0 {
		line += " (" + formatElapsed(elapsed) + ")"
	}
	return line
}

// FormatOutcomeSummary renders the compact completion message used by the
// normal submit experience. It deliberately omits unchanged branches and the
// per-branch audit trail; --verbose retains those details.
func FormatOutcomeSummary(items []Item, elapsed time.Duration) string {
	var created, updated, failed int
	for _, item := range items {
		switch {
		case item.Status == StatusError:
			failed++
		case item.Status == StatusDone && item.Action == ActionCreate:
			created++
		case item.Status == StatusDone && item.Action == ActionUpdate:
			updated++
		}
	}

	parts := make([]string, 0, 3)
	if created > 0 {
		parts = append(parts, fmt.Sprintf("opened %d %s", created, pluralPR(created)))
	}
	if updated > 0 {
		parts = append(parts, fmt.Sprintf("updated %d %s", updated, pluralPR(updated)))
	}
	if failed > 0 {
		parts = append(parts, fmt.Sprintf("%d %s failed", failed, pluralPR(failed)))
	}
	if len(parts) == 0 {
		return ""
	}

	icon := "✓"
	if failed > 0 {
		icon = "✗"
	}
	line := icon + " " + strings.ToUpper(parts[0][:1]) + parts[0][1:]
	if len(parts) > 1 {
		line += " · " + strings.Join(parts[1:], " · ")
	}
	if elapsed > 0 {
		line += " (" + formatElapsed(elapsed) + ")"
	}
	return line
}

func pluralPR(count int) string {
	if count == 1 {
		return "PR"
	}
	return "PRs"
}

// formatElapsed renders a duration compactly: "6.2s" under a minute, "1m23s" above.
func formatElapsed(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	return d.Round(time.Second).String()
}

// FormatFailureSummary renders failed branches with their errors. The progress
// view is cleared when the TUI exits, so failures must be re-emitted as
// persistent output or they vanish from the terminal.
func FormatFailureSummary(items []Item) string {
	rows := make([]string, 0, len(items))
	for _, item := range items {
		if item.Status != StatusError {
			continue
		}
		rows = append(rows, fmt.Sprintf("✗ %s — %s", DisplayBranchName(item.BranchName), errorText(item.Error)))
	}
	if len(rows) == 0 {
		return ""
	}
	return strings.Join(rows, "\n")
}
