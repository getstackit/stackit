// Package submit provides a TUI component for displaying the progress of a stack submission.
package submit

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"stackit.dev/stackit/internal/tui/components/tree"
)

// Model is the bubbletea model for submit progress
type Model struct {
	Items         []Item
	Renderer      *tree.StackTreeRenderer
	RootBranch    string
	Spinner       spinner.Model
	Done          bool
	Styles        Styles
	GlobalMessage string
}

// ProgressUpdateMsg is sent to update the status of a specific branch submission
type ProgressUpdateMsg struct {
	BranchName string
	Status     string
	URL        string
	Err        error
}

// StartSubmitMsg is sent when the submission phase begins
type StartSubmitMsg struct {
	Items []Item
}

// PlanUpdateMsg is sent when a branch plan is updated
type PlanUpdateMsg struct {
	BranchName string
	Action     string
	IsCurrent  bool
	Skip       bool
	SkipReason string
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
		Spinner: s,
		Styles:  DefaultStyles(),
	}
}

// Init initializes the model.
func (m *Model) Init() tea.Cmd {
	return m.Spinner.Tick
}

// Update handles messages and updates the model.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" || msg.String() == "q" {
			return m, tea.Quit
		}

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.Spinner, cmd = m.Spinner.Update(msg)
		return m, cmd

	case StartSubmitMsg:
		// Update status for items that are in msg.Items
		for _, newItem := range msg.Items {
			found := false
			for i, item := range m.Items {
				if item.BranchName == newItem.BranchName {
					m.Items[i].Status = newItem.Status
					m.Items[i].Action = newItem.Action
					m.Items[i].PRNumber = newItem.PRNumber
					found = true
					break
				}
			}
			if !found {
				m.Items = append(m.Items, newItem)
			}
		}
		return m, nil

	case PlanUpdateMsg:
		// Update existing item or add new one
		found := false
		for i, item := range m.Items {
			if item.BranchName == msg.BranchName {
				m.Items[i].Action = msg.Action
				m.Items[i].IsSkipped = msg.Skip
				m.Items[i].SkipReason = msg.SkipReason
				found = true
				break
			}
		}
		if !found {
			m.Items = append(m.Items, Item{
				BranchName: msg.BranchName,
				Action:     msg.Action,
				IsSkipped:  msg.Skip,
				SkipReason: msg.SkipReason,
				Status:     StatusPending,
			})
		}
		return m, nil

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
		return m, m.Spinner.Tick

	case ProgressCompleteMsg:
		m.Done = true
		return m, tea.Quit
	}

	return m, nil
}

// View renders the model as a string.
func (m *Model) View() string {
	var b strings.Builder

	if m.Renderer != nil {
		// Update annotations based on items
		for _, item := range m.Items {
			ann := m.Renderer.Annotations[item.BranchName]

			// Update PR action if known
			if item.Action != "" {
				ann.PRAction = item.Action
			}

			// Update custom label for status
			if item.IsSkipped {
				if item.SkipReason == SkipReasonNoChanges {
					ann.PRAction = SkipReasonNoChanges
					ann.CustomLabel = ""
				} else {
					ann.CustomLabel = m.Styles.DimStyle.Render("(skipped: " + item.SkipReason + ")")
				}
			} else {
				switch item.Status {
				case StatusSubmitting:
					ann.CustomLabel = m.Styles.SpinnerStyle.Render(m.Spinner.View() + " submitting...")
				case StatusSyncing:
					ann.CustomLabel = m.Styles.SpinnerStyle.Render(m.Spinner.View() + " syncing...")
				case StatusDone:
					ann.CustomLabel = m.Styles.DoneStyle.Render("✓")
					if item.URL != "" {
						ann.CustomLabel += " " + m.Styles.URLStyle.Render("→ "+item.URL)
					}
				case StatusError:
					ann.CustomLabel = m.Styles.ErrorStyle.Render("✗")
					if item.Error != nil {
						ann.CustomLabel += " " + m.Styles.ErrorStyle.Render(item.Error.Error())
					}
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
			case StatusPending, "":
				icon = m.Styles.DimStyle.Render("○")
				if item.IsSkipped {
					if item.SkipReason == SkipReasonNoChanges {
						status = m.Styles.DimStyle.Render(SkipReasonNoChanges)
					} else {
						status = m.Styles.DimStyle.Render("skipped (" + item.SkipReason + ")")
					}
				} else {
					status = m.Styles.DimStyle.Render("will " + item.Action)
				}
			case StatusSubmitting:
				icon = m.Spinner.View()
				action := "Creating"
				if item.Action == "update" {
					action = "Updating"
				}
				status = m.Styles.SpinnerStyle.Render(action + "...")
			case StatusSyncing:
				icon = m.Spinner.View()
				status = m.Styles.SpinnerStyle.Render("syncing...")
			case StatusDone:
				icon = m.Styles.DoneStyle.Render("✓")
				status = m.Styles.DoneStyle.Render(item.Action + "ed")
			case StatusError:
				icon = m.Styles.ErrorStyle.Render("✗")
				status = m.Styles.ErrorStyle.Render("failed")
			}

			branchName := m.Styles.BranchStyle.Render(item.BranchName)
			line := fmt.Sprintf("  %s %s %s", icon, branchName, status)

			if item.Status == StatusDone && item.URL != "" {
				line += " " + m.Styles.URLStyle.Render("→ "+item.URL)
			}
			if item.Status == StatusError && item.Error != nil {
				line += " " + m.Styles.ErrorStyle.Render(item.Error.Error())
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
			if item.Status == StatusDone {
				completed++
			} else if item.Status == StatusError {
				failed++
			}
		}
		if failed > 0 {
			b.WriteString("\n\n")
			b.WriteString(m.Styles.ErrorStyle.Render(fmt.Sprintf("Completed: %d, Failed: %d", completed, failed)))
		}
	}

	b.WriteString("\n")
	return b.String()
}
