// Package tui provides the terminal user interface for stackit.
//
// It handles:
//   - Interactive prompts and selections (using survey and bubbletea)
//   - Structured logging and status reporting (Splog)
//   - Terminal styling and colors (using lipgloss)
//   - Progress indicators and UI components
package tui

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"stackit.dev/stackit/internal/tui/components/submit"
	"stackit.dev/stackit/internal/tui/style"
	"stackit.dev/stackit/internal/utils"
)

// Key constants for TUI interactions
const (
	KeyCtrlC = "ctrl+c"
	KeyQuit  = "q"
	KeyEsc   = "esc"
	KeyEnter = "enter"
	KeyUp    = "up"
	KeyDown  = "down"
	KeyTab   = "tab"
)

// SubmitTUIModel is the bubbletea model for submit progress
type SubmitTUIModel struct {
	items      []submit.Item
	currentIdx int
	spinner    spinner.Model
	done       bool
	quitting   bool
	submitFunc func(idx int) tea.Cmd
	styles     submitStyles
}

type submitStyles struct {
	spinnerStyle lipgloss.Style
	doneStyle    lipgloss.Style
	errorStyle   lipgloss.Style
	branchStyle  lipgloss.Style
	urlStyle     lipgloss.Style
	dimStyle     lipgloss.Style
}

// SubmitResultMsg is sent when a single submit completes
type SubmitResultMsg struct {
	Idx   int
	URL   string
	Error error
}

// AllDoneMsg signals all submissions are complete
type AllDoneMsg struct{}

// NewSubmitTUIModel creates a new submit TUI model
func NewSubmitTUIModel(items []submit.Item, submitFunc func(idx int) tea.Cmd) SubmitTUIModel {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))

	return SubmitTUIModel{
		items:      items,
		currentIdx: 0,
		spinner:    s,
		submitFunc: submitFunc,
		styles: submitStyles{
			spinnerStyle: lipgloss.NewStyle().Foreground(lipgloss.Color("205")),
			doneStyle:    lipgloss.NewStyle().Foreground(lipgloss.Color("42")),
			errorStyle:   lipgloss.NewStyle().Foreground(lipgloss.Color("196")),
			branchStyle:  style.BranchStyle(false, false, false).Bold(true),
			urlStyle:     lipgloss.NewStyle().Foreground(lipgloss.Color("245")),
			dimStyle:     lipgloss.NewStyle().Foreground(lipgloss.Color("240")),
		},
	}
}

const (
	statusPending    = "pending"
	statusSubmitting = "submitting"
	statusDone       = "done"
	statusError      = "error"
	actionUpdate     = "update"
)

// Init initializes the bubbletea model
func (m SubmitTUIModel) Init() tea.Cmd {
	// Start spinner and first submission
	if len(m.items) > 0 {
		m.items[0].Status = statusSubmitting
		return tea.Batch(m.spinner.Tick, m.submitFunc(0))
	}
	return nil
}

// Update handles message updates for the bubbletea model
func (m SubmitTUIModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if msg, ok := msg.(tea.KeyMsg); ok {
		if msg.String() == KeyCtrlC || msg.String() == KeyQuit {
			m.quitting = true
			return m, tea.Quit
		}
	}

	switch msg := msg.(type) {
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case SubmitResultMsg:
		if msg.Idx < len(m.items) {
			if msg.Error != nil {
				m.items[msg.Idx].Status = statusError
				m.items[msg.Idx].Error = msg.Error
			} else {
				m.items[msg.Idx].Status = statusDone
				m.items[msg.Idx].URL = msg.URL
			}
		}

		// Move to next item
		m.currentIdx++
		if m.currentIdx < len(m.items) {
			m.items[m.currentIdx].Status = statusSubmitting
			return m, tea.Batch(m.spinner.Tick, m.submitFunc(m.currentIdx))
		}

		// All done
		m.done = true
		return m, tea.Quit

	case AllDoneMsg:
		m.done = true
		return m, tea.Quit
	}

	return m, nil
}

// View renders the bubbletea model
func (m SubmitTUIModel) View() string {
	if m.quitting {
		return ""
	}

	var b strings.Builder
	b.WriteString("\n")

	for i, item := range m.items {
		var icon string
		var status string

		switch item.Status {
		case statusPending:
			icon = m.styles.dimStyle.Render("○")
			status = m.styles.dimStyle.Render("pending")
		case statusSubmitting:
			icon = m.spinner.View()
			action := "Creating"
			if item.Action == actionUpdate {
				action = "Updating"
			}
			status = m.styles.spinnerStyle.Render(action + "...")
		case statusDone:
			icon = m.styles.doneStyle.Render("✓")
			action := "created"
			if item.Action == actionUpdate {
				action = "updated"
			}
			status = m.styles.doneStyle.Render(action)
		case statusError:
			icon = m.styles.errorStyle.Render("✗")
			status = m.styles.errorStyle.Render("failed")
		}

		branchName := m.styles.branchStyle.Render(item.BranchName)
		line := fmt.Sprintf("  %s %s %s", icon, branchName, status)

		if item.Status == statusDone && item.URL != "" {
			line += " " + m.styles.urlStyle.Render("→ "+item.URL)
		}
		if item.Status == statusError && item.Error != nil {
			line += " " + m.styles.errorStyle.Render(item.Error.Error())
		}

		b.WriteString(line)
		if i < len(m.items)-1 {
			b.WriteString("\n")
		}
	}

	b.WriteString("\n")

	if m.done {
		completed := 0
		failed := 0
		for _, item := range m.items {
			if item.Status == statusDone {
				completed++
			} else if item.Status == statusError {
				failed++
			}
		}
		b.WriteString("\n")
		if failed > 0 {
			b.WriteString(m.styles.errorStyle.Render(fmt.Sprintf("Completed: %d, Failed: %d", completed, failed)))
		} else {
			b.WriteString(m.styles.doneStyle.Render(fmt.Sprintf("✓ All %d PRs submitted successfully", completed)))
		}
		b.WriteString("\n")
	}

	return b.String()
}

// RunSubmitTUI runs the submit TUI and returns when complete
func RunSubmitTUI(items []submit.Item, submitFunc func(idx int) tea.Cmd) error {
	if !utils.IsInteractive() {
		// This should be handled by the caller using RunSubmitTUISimple
		return fmt.Errorf("RunSubmitTUI called in non-interactive mode")
	}
	m := NewSubmitTUIModel(items, submitFunc)
	// Use WithInput/WithOutput to avoid TTY requirement in non-interactive environments
	p := tea.NewProgram(m, tea.WithInput(os.Stdin), tea.WithOutput(os.Stdout))
	_, err := p.Run()
	return err
}

// RunSubmitTUISimple runs a simple non-interactive version for non-TTY environments
func RunSubmitTUISimple(items []submit.Item, submitFunc func(idx int) (string, error), splog *Splog) error {
	const (
		actionUpdate  = "update"
		actionCreated = "created"
		actionUpdated = "updated"
	)
	for i, item := range items {
		action := "Creating"
		if item.Action == actionUpdate {
			action = "Updating"
		}
		splog.Info("  ⋯ %s %s...", item.BranchName, action)

		url, err := submitFunc(i)
		if err != nil {
			splog.Info("  ✗ %s failed: %v", item.BranchName, err)
			return err
		}

		actionDone := actionCreated
		if item.Action == actionUpdate {
			actionDone = actionUpdated
		}
		splog.Info("  ✓ %s %s → %s", item.BranchName, actionDone, url)
	}
	return nil
}

// IsTTY returns true if we can use a TTY for interactive TUI.
// Re-export from utils for backward compatibility.
func IsTTY() bool {
	return utils.IsTTY()
}

// SetInteractive sets whether the TUI should be interactive.
// Re-export from utils for backward compatibility.
func SetInteractive(interactive bool) {
	utils.SetInteractive(interactive)
	if !interactive {
		lipgloss.SetColorProfile(termenv.Ascii)
	}
}
