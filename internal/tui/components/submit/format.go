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
		ref := PRRef(item)
		if ref == "" {
			ref = pastTense(item.Action)
		}
		return styles.DoneStyle.Render("✓"), styles.DoneStyle.Render(ref)
	case StatusError:
		detail := "failed"
		if item.Error != nil {
			detail = item.Error.Error()
		}
		return styles.ErrorStyle.Render("✗"), styles.ErrorStyle.Render(detail)
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

func actionLabel(action string) string {
	switch action {
	case ActionCreate:
		return "create"
	case ActionUpdate:
		return "update"
	case "thinking...":
		return "planning"
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

// FormatCompletionSummary renders the persistent post-submit output: PR URLs
// for submitted branches followed by errors for failed ones.
func FormatCompletionSummary(items []Item) string {
	sections := make([]string, 0, 2)
	if urls := FormatURLSummary(items); urls != "" {
		sections = append(sections, urls)
	}
	if failures := FormatFailureSummary(items); failures != "" {
		sections = append(sections, failures)
	}
	return strings.Join(sections, "\n\n")
}
