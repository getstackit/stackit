// Package submit provides a TUI component for displaying the progress of a stack submission.
package submit

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"

	"github.com/getstackit/stackit/internal/tui/core"
)

// Model is the bubbletea model for submit progress.
// It embeds core.BaseModel for standard lifecycle handling.
type Model struct {
	core.BaseModel // Embedded for ReadySignaler interface
	Items          []Item
	spinner        spinner.Model // lowercase for custom style
	Styles         Styles
	GlobalMessage  string
}

// ProgressUpdateMsg is sent to update the status of a specific branch submission
type ProgressUpdateMsg struct {
	BranchName string
	Status     string
	URL        string
	Err        error
}

// GlobalMessageMsg is sent to display a global message (e.g., "Submitting...")
type GlobalMessageMsg string

// ProgressCompleteMsg is sent when all submissions are finished
type ProgressCompleteMsg struct{}

// NewModel creates a new submit model
func NewModel(items []Item) *Model {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = DefaultStyles().SpinnerStyle

	return &Model{
		Items:   items,
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
	case GlobalMessageMsg:
		m.GlobalMessage = string(msg)
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
		summary := m.completionSummary()
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
		b.WriteString(FormatCompactRow(item, width, m.spinner.View(), m.Styles))
		if i < len(m.Items)-1 {
			b.WriteString("\n")
		}
	}

	return b.String()
}

// completionSummary is the output persisted to the terminal when the TUI
// exits. After a submission it lists PR URLs and failures; when nothing was
// submitted (dry run, all up to date) it falls back to the final plan view,
// which would otherwise be erased with the progress display.
func (m *Model) completionSummary() string {
	if summary := FormatCompletionSummary(m.Items); summary != "" {
		return summary
	}
	return m.content()
}

func (m *Model) header() string {
	message := strings.TrimSpace(m.GlobalMessage)
	count := len(m.Items)
	if message == "" {
		if count == 0 {
			return ""
		}
		return fmt.Sprintf("Submit %d %s", count, pluralBranch(count))
	}

	if strings.TrimSuffix(message, "...") == "Submitting" {
		return fmt.Sprintf("Submitting %d %s", count, pluralBranch(count))
	}
	return message
}

func pluralBranch(count int) string {
	if count == 1 {
		return "branch"
	}
	return "branches"
}
