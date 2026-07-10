package submit

import (
	"fmt"
	"net/url"
	"path"
	"strconv"
	"strings"
	"unicode"

	"charm.land/lipgloss/v2"
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
		return styles.DimStyle.Render("○"), styles.DimStyle.Render(item.Status)
	}
}

// maxErrorDetailWidth caps the error text shown in a progress row so a long
// git/gh message cannot shred the column layout mid-TUI.
const maxErrorDetailWidth = 60

// compactErrorText flattens an error to its trimmed first line, truncated to
// fit a progress row. The full text still persists via FormatFailureSummary
// when the TUI exits, so nothing is lost.
func compactErrorText(err error) string {
	if err == nil {
		return "failed"
	}
	detail := err.Error()
	if i := strings.IndexByte(detail, '\n'); i >= 0 {
		detail = detail[:i]
	}
	detail = strings.TrimSpace(detail)
	if detail == "" {
		return "failed"
	}
	return TruncateMiddle(detail, maxErrorDetailWidth)
}

func actionLabel(action string) string {
	switch action {
	case ActionCreate:
		return "create"
	case ActionUpdate:
		return "update"
	default:
		return action
	}
}

func pastTense(action string) string {
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

// FormatURLSummary renders clickable PR URLs after submit completes.
func FormatURLSummary(items []Item) string {
	rows := make([]string, 0, len(items))
	for _, item := range items {
		if item.URL == "" {
			continue
		}
		ref := PRRef(item)
		if ref == "" {
			ref = "-"
		}
		name := DisplayBranchName(item.BranchName)
		rows = append(rows, fmt.Sprintf("%s %s\n     %s", ref, name, item.URL))
	}
	if len(rows) == 0 {
		return ""
	}
	return "Pull requests\n\n" + strings.Join(rows, "\n")
}

// hyperlink wraps text in an OSC 8 terminal hyperlink escape sequence.
// Terminals without OSC 8 support ignore the sequence and show the plain text.
func hyperlink(url, text string) string {
	return "\x1b]8;;" + url + "\x1b\\" + text + "\x1b]8;;\x1b\\"
}

// FormatLinkedURLSummary renders the post-submit PR list with clickable labels
// and visible URLs. Keeping the URL visible matters for copy/paste, logs, and
// terminals that do not expose OSC 8 links clearly.
func FormatLinkedURLSummary(items []Item) string {
	rows := make([]string, 0, len(items))
	for _, item := range items {
		if item.URL == "" {
			continue
		}
		ref := PRRef(item)
		if ref == "" {
			ref = "-"
		}
		name := DisplayBranchName(item.BranchName)
		rows = append(rows, fmt.Sprintf("%s\n     %s", hyperlink(item.URL, fmt.Sprintf("%s %s", ref, name)), item.URL))
	}
	if len(rows) == 0 {
		return ""
	}
	return "Pull requests\n\n" + strings.Join(rows, "\n")
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

// FormatFailureSummary renders failed branches with their errors. The progress
// view is cleared when the TUI exits, so failures must be re-emitted as
// persistent output or they vanish from the terminal.
func FormatFailureSummary(items []Item) string {
	rows := make([]string, 0, len(items))
	for _, item := range items {
		if item.Status != StatusError {
			continue
		}
		detail := "failed"
		if item.Error != nil {
			detail = item.Error.Error()
		}
		rows = append(rows, fmt.Sprintf("✗ %s — %s", DisplayBranchName(item.BranchName), detail))
	}
	if len(rows) == 0 {
		return ""
	}
	return strings.Join(rows, "\n")
}
