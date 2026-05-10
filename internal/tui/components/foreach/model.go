// Package foreach provides a TUI component for displaying the progress of foreach command execution.
package foreach

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"

	"github.com/getstackit/stackit/internal/tui/components/tree"
	"github.com/getstackit/stackit/internal/tui/core"
)

// Model is the bubbletea model for foreach progress.
// It embeds core.BaseModel for standard lifecycle handling.
type Model struct {
	core.BaseModel // Embedded for ReadySignaler interface
	Items          []Item
	Renderer       *tree.StackTreeRenderer
	RootBranch     string
	spinner        spinner.Model // Use local spinner for custom style
	Styles         Styles
	GlobalMessage  string
	Command        string
}

// ProgressUpdateMsg is sent to update the status of a specific branch execution
type ProgressUpdateMsg struct {
	BranchName string
	Status     core.Status
	Output     string
	Err        error
}

// StartExecutionMsg is sent when the execution phase begins
type StartExecutionMsg struct {
	Items []Item
}

// GlobalMessageMsg is sent to display a global message (e.g., "Running...")
type GlobalMessageMsg string

// ProgressCompleteMsg is sent when all executions are finished
type ProgressCompleteMsg struct{}

// NewModel creates a new foreach model
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
	case StartExecutionMsg:
		// Update status for items that are in msg.Items
		for _, newItem := range msg.Items {
			found := false
			for i, item := range m.Items {
				if item.BranchName == newItem.BranchName {
					m.Items[i].Status = newItem.Status
					found = true
					break
				}
			}
			if !found {
				m.Items = append(m.Items, newItem)
			}
		}
		return m, nil

	case GlobalMessageMsg:
		m.GlobalMessage = string(msg)
		return m, nil

	case ProgressUpdateMsg:
		for i, item := range m.Items {
			if item.BranchName == msg.BranchName {
				m.Items[i].Status = msg.Status
				m.Items[i].Output = msg.Output
				m.Items[i].Error = msg.Err
				break
			}
		}
		return m, m.spinner.Tick

	case ProgressCompleteMsg:
		m.MarkDone()
		return m, tea.Quit
	}

	return m, nil
}

// View renders the model as a string.
func (m *Model) View() tea.View {
	var b strings.Builder

	if m.Command != "" {
		b.WriteString(m.Styles.DimStyle.Render("Command: " + m.Command))
		b.WriteString("\n\n")
	}

	if m.GlobalMessage != "" {
		b.WriteString(m.Styles.DimStyle.Render(m.GlobalMessage))
		b.WriteString("\n\n")
	}

	if m.Renderer != nil {
		// Update annotations based on items
		for _, item := range m.Items {
			ann := m.Renderer.Annotations[item.BranchName]

			// Update custom label for status
			switch item.Status {
			case core.StatusActive:
				ann.CustomLabel = m.Styles.SpinnerStyle.Render(m.spinner.View() + " running...")
			case core.StatusDone:
				ann.CustomLabel = m.Styles.DoneStyle.Render("✓")
				if item.Output != "" {
					// Show truncated output
					output := strings.TrimSpace(item.Output)
					if len(output) > 50 {
						output = output[:47] + "..."
					}
					ann.CustomLabel += " " + m.Styles.OutputStyle.Render(output)
				}
			case core.StatusError:
				ann.CustomLabel = m.Styles.ErrorStyle.Render("✗")
				if item.Error != nil {
					errMsg := item.Error.Error()
					if len(errMsg) > 50 {
						errMsg = errMsg[:47] + "..."
					}
					ann.CustomLabel += " " + m.Styles.ErrorStyle.Render(errMsg)
				}
			}
			m.Renderer.SetAnnotation(item.BranchName, ann)
		}

		lines := m.Renderer.RenderStack(m.RootBranch, tree.RenderOptions{
			HideStats: true,
		})
		b.WriteString(strings.Join(lines, "\n"))
	} else {
		// Fallback to list view if no renderer
		for i, item := range m.Items {
			var icon string
			var status string

			switch item.Status {
			case core.StatusPending, "":
				icon = m.Styles.DimStyle.Render("○")
				status = m.Styles.DimStyle.Render("pending")
			case core.StatusActive:
				icon = m.spinner.View()
				status = m.Styles.SpinnerStyle.Render("running...")
			case core.StatusDone:
				icon = m.Styles.DoneStyle.Render("✓")
				status = m.Styles.DoneStyle.Render("done")
			case core.StatusError:
				icon = m.Styles.ErrorStyle.Render("✗")
				status = m.Styles.ErrorStyle.Render("failed")
			}

			branchName := m.Styles.BranchStyle.Render(item.BranchName)
			line := fmt.Sprintf("  %s %s %s", icon, branchName, status)

			if item.Status == core.StatusDone && item.Output != "" {
				output := strings.TrimSpace(item.Output)
				if len(output) > 50 {
					output = output[:47] + "..."
				}
				line += " " + m.Styles.OutputStyle.Render(output)
			}
			if item.Status == core.StatusError && item.Error != nil {
				errMsg := item.Error.Error()
				if len(errMsg) > 50 {
					errMsg = errMsg[:47] + "..."
				}
				line += " " + m.Styles.ErrorStyle.Render(errMsg)
			}

			b.WriteString(line)
			if i < len(m.Items)-1 {
				b.WriteString("\n")
			}
		}
	}

	if m.Done {
		completed := 0
		failed := 0
		for _, item := range m.Items {
			switch item.Status {
			case core.StatusDone:
				completed++
			case core.StatusError:
				failed++
			}
		}
		if failed > 0 {
			b.WriteString("\n\n")
			b.WriteString(m.Styles.ErrorStyle.Render(fmt.Sprintf("Completed: %d, Failed: %d", completed, failed)))
		}
	}

	b.WriteString("\n")
	return tea.NewView(b.String())
}
