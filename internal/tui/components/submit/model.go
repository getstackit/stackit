// Package submit provides a TUI component for displaying the progress of a stack submission.
package submit

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"

	"github.com/getstackit/stackit/internal/tui/core"
	"github.com/getstackit/stackit/internal/tui/style"
)

// Model is the bubbletea model for submit progress.
// It embeds core.BaseModel for standard lifecycle handling.
type Model struct {
	core.BaseModel // Embedded for ReadySignaler interface
	Items          []Item
	Warnings       []string      // formatted warning lines, rendered after the rows and persisted on exit
	Solo           bool          // single-branch submit — drop the count header and per-row name
	Verbose        bool          // retain the detailed final per-branch result list
	spinner        spinner.Model // lowercase for custom style
	Styles         Styles
}

// ProgressUpdateMsg is sent to update the status of a specific branch submission
type ProgressUpdateMsg struct {
	BranchName string
	Status     Status
	URL        string
	Err        error
}

// WarningMsg surfaces a non-fatal warning for a branch (e.g. labels could not
// be applied). Warnings render below the progress rows and persist on exit.
type WarningMsg struct {
	BranchName string
	Warning    string
}

// ProgressCompleteMsg is sent when all submissions are finished.
type ProgressCompleteMsg struct {
	Skipped int           // branches skipped in the plan, shown as "unchanged"
	Elapsed time.Duration // total run time; zero when unknown
}

// NewModel creates a new submit model
func NewModel(items []Item) *Model {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = DefaultStyles().SpinnerStyle

	return &Model{
		Items:   items,
		Verbose: true,
		spinner: s,
		Styles:  DefaultStyles(),
	}
}

// Init initializes the model.
func (m *Model) Init() tea.Cmd {
	// Signal that the program is ready to receive messages via BaseModel
	m.SignalReady()
	return m.spinner.Tick
}

// Update handles messages and updates the model.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Handle spinner ticks with our custom spinner BEFORE HandleCommonMsg
	if tickMsg, ok := msg.(spinner.TickMsg); ok {
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(tickMsg)
		return m, cmd
	}

	// Handle common messages via BaseModel (key events, window resize)
	if handled, cmd := m.HandleCommonMsg(msg); handled {
		return m, cmd
	}

	switch msg := msg.(type) {
	case WarningMsg:
		m.Warnings = append(m.Warnings, fmt.Sprintf("⚠️  %s: %s", style.DisplayBranchName(msg.BranchName), msg.Warning))
		return m, nil

	case ProgressUpdateMsg:
		for i, item := range m.Items {
			if item.BranchName == msg.BranchName {
				m.Items[i].Status = msg.Status
				// Only update URL/Error if new values are provided (preserve existing)
				if msg.URL != "" {
					m.Items[i].URL = msg.URL
				}
				if msg.Err != nil {
					m.Items[i].Error = msg.Err
				}
				break
			}
		}
		return m, m.spinner.Tick

	case ProgressCompleteMsg:
		m.Done = true
		var summary string
		if m.Verbose {
			summary = m.completionSummary()
			// The solo summary already names the single result; a count line
			// would just restate it.
			if !m.Solo && summary != "" {
				if closing := FormatClosingSummary(m.Items, msg.Skipped, msg.Elapsed); closing != "" {
					summary += "\n\n" + closing
				}
			}
		} else {
			summary = FormatOutcomeSummary(m.Items, msg.Elapsed)
			if urls := FormatCreatedURLs(m.Items); urls != "" {
				if summary != "" {
					summary += "\n"
				}
				summary += urls
			}
			if failures := FormatFailureSummary(m.Items); failures != "" {
				if summary != "" {
					summary += "\n\n"
				}
				summary += failures
			}
		}
		if summary != "" {
			return m, tea.Sequence(
				tea.Printf("\n%s", summary),
				tea.Quit,
			)
		}
		return m, tea.Quit
	}

	return m, nil
}

// View renders the model as a string.
func (m *Model) View() tea.View {
	if m.Done {
		return tea.NewView("")
	}
	return tea.NewView(m.content() + "\n")
}

// content renders the header and per-branch progress rows.
func (m *Model) content() string {
	var b strings.Builder

	header := m.header()
	if header != "" {
		b.WriteString(header)
		b.WriteString("\n\n")
	}

	width := m.Width
	if width == 0 {
		width = defaultSubmitWidth
	}
	for i, item := range m.Items {
		if m.Solo {
			b.WriteString(FormatSoloRow(item, m.spinner.View(), m.Styles))
		} else {
			b.WriteString(FormatCompactRow(item, width, m.spinner.View(), m.Styles))
		}
		if i < len(m.Items)-1 {
			b.WriteString("\n")
		}
	}

	for _, warning := range m.Warnings {
		b.WriteString("\n")
		b.WriteString(warning)
	}

	return b.String()
}

// completionSummary is the verbose-mode output persisted to the terminal when
// the TUI exits. After a submission it lists every branch's final result
// (including failures); when nothing was submitted (dry run, all up to date) it
// falls back to the final plan view, which would otherwise be erased with the
// progress display. Warnings are appended in either case so they survive the
// screen clear. Compact mode builds its own summary inline in the
// ProgressCompleteMsg handler and does not call this.
func (m *Model) completionSummary() string {
	var summary string
	if m.Solo {
		summary = FormatSoloSummary(m.Items)
		if failures := FormatFailureSummary(m.Items); failures != "" {
			if summary != "" {
				summary += "\n\n"
			}
			summary += failures
		}
	} else {
		summary = FormatFinalList(m.Items)
	}
	if summary == "" {
		return m.content()
	}
	if len(m.Warnings) > 0 {
		summary += "\n\n" + strings.Join(m.Warnings, "\n")
	}
	return summary
}

func (m *Model) header() string {
	// A solo submit is framed by the plan line printed above the TUI; a count
	// header would just restate it.
	if m.Solo {
		return ""
	}
	count := len(m.Items)
	if count == 0 {
		return ""
	}
	if m.Verbose {
		return fmt.Sprintf("Submitting %d %s", count, pluralBranch(count))
	}
	return fmt.Sprintf("Submitting %d %s", count, pluralPR(count))
}

func pluralBranch(count int) string {
	if count == 1 {
		return "branch"
	}
	return "branches"
}
