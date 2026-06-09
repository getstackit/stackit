// Package submit provides a TUI component for displaying the progress of a stack submission.
package submit

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"

	"github.com/getstackit/stackit/internal/tui/components/tree"
	"github.com/getstackit/stackit/internal/tui/core"
)

// Model is the bubbletea model for submit progress.
// It embeds core.BaseModel for standard lifecycle handling.
type Model struct {
	core.BaseModel // Embedded for ReadySignaler interface
	Items          []Item
	Renderer       *tree.StackTreeRenderer
	RootBranch     string
	spinner        spinner.Model // lowercase for custom style
	Styles         Styles
	GlobalMessage  string
	IsSequential   bool // Sequential submission mode for PR ordering
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

// SetSequentialMsg indicates sequential submission mode for PR ordering
type SetSequentialMsg struct {
	IsSequential bool
}

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
	case SetSequentialMsg:
		m.IsSequential = msg.IsSequential
		return m, nil

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
		return m, m.spinner.Tick

	case ProgressCompleteMsg:
		m.Done = true
		summary := FormatCompletionSummary(m.Items)
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
					ann.CustomLabel = m.Styles.SpinnerStyle.Render(m.spinner.View() + " submitting...")
				case StatusSyncing:
					ann.CustomLabel = m.Styles.SpinnerStyle.Render(m.spinner.View() + " syncing...")
				case StatusDone:
					ann.PRAction = "" // Clear action since we're showing the result
					ann.CustomLabel = m.Styles.DoneStyle.Render("✓")
					if item.URL != "" {
						ann.CustomLabel += " " + m.Styles.URLStyle.Render("→ "+item.URL)
					}
				case StatusError:
					ann.PRAction = "" // Clear action since we're showing the result
					ann.CustomLabel = m.Styles.ErrorStyle.Render("✗")
					if item.Error != nil {
						ann.CustomLabel += " " + m.Styles.ErrorStyle.Render(item.Error.Error())
					}
				}
			}
			m.Renderer.SetAnnotation(item.BranchName, ann)
		}
	}

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

	b.WriteString("\n")
	return tea.NewView(b.String())
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

	switch strings.TrimSuffix(message, "...") {
	case "Submitting":
		return fmt.Sprintf("Submitting %d %s", count, pluralBranch(count))
	case "Preparing branches":
		return fmt.Sprintf("Preparing %d %s", count, pluralBranch(count))
	case "Restacking branches":
		return fmt.Sprintf("Restacking %d %s", count, pluralBranch(count))
	default:
		return message
	}
}

func pluralBranch(count int) string {
	if count == 1 {
		return "branch"
	}
	return "branches"
}
