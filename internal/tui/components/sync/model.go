// Package sync provides a TUI component for displaying sync progress.
package sync

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"stackit.dev/stackit/internal/tui/style"
)

// Phase represents a sync phase
type Phase string

// Phase constants
const (
	PhaseTrunk   Phase = "trunk"
	PhaseGitHub  Phase = "github"
	PhaseClean   Phase = "clean"
	PhaseRestack Phase = "restack"
)

// Status constants
const (
	StatusPending = "pending"
	StatusActive  = "active"
	StatusDone    = "done"
)

// PhaseItem represents progress for a single phase
type PhaseItem struct {
	Phase   Phase
	Status  string // StatusPending, StatusActive, StatusDone
	Message string
	Details []string // Lines of detail output for this phase
}

// Model is the bubbletea model for sync progress
type Model struct {
	Phases       []PhaseItem
	CurrentPhase Phase
	TotalOps     int
	CompletedOps int
	Progress     progress.Model
	Spinner      spinner.Model
	Done         bool
	Summary      string
	Width        int
}

// PhaseStartMsg indicates a phase has started
type PhaseStartMsg struct {
	Phase Phase
}

// PhaseDetailMsg adds a detail line to a phase
type PhaseDetailMsg struct {
	Phase   Phase
	Message string
}

// ProgressTickMsg updates the progress bar
type ProgressTickMsg struct {
	Completed int
	Total     int
}

// CompleteMsg indicates sync is complete
type CompleteMsg struct {
	Summary string
}

// NewModel creates a new sync model
func NewModel(totalOps int) *Model {
	p := progress.New(
		progress.WithDefaultGradient(),
		progress.WithWidth(40),
		progress.WithoutPercentage(),
	)

	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))

	return &Model{
		Phases: []PhaseItem{
			{Phase: PhaseTrunk, Status: StatusPending, Message: "📥 Pulling from remote..."},
			{Phase: PhaseGitHub, Status: StatusPending, Message: "🔄 Fetching PR info from GitHub..."},
			{Phase: PhaseClean, Status: StatusPending, Message: "🧹 Cleaning branches..."},
			{Phase: PhaseRestack, Status: StatusPending, Message: "📚 Restacking branches..."},
		},
		TotalOps: totalOps,
		Progress: p,
		Spinner:  s,
		Width:    80,
	}
}

// Init initializes the model
func (m *Model) Init() tea.Cmd {
	return m.Spinner.Tick
}

// Update handles messages
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" || msg.String() == "q" {
			return m, tea.Quit
		}

	case tea.WindowSizeMsg:
		m.Width = msg.Width
		newWidth := msg.Width - 10
		if newWidth > 60 {
			newWidth = 60
		}
		m.Progress.Width = newWidth
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.Spinner, cmd = m.Spinner.Update(msg)
		return m, cmd

	case progress.FrameMsg:
		progressModel, cmd := m.Progress.Update(msg)
		m.Progress = progressModel.(progress.Model)
		return m, cmd

	case PhaseStartMsg:
		m.CurrentPhase = msg.Phase
		for i := range m.Phases {
			if m.Phases[i].Phase == msg.Phase {
				m.Phases[i].Status = StatusActive
			} else if m.Phases[i].Status == StatusActive {
				m.Phases[i].Status = StatusDone
			}
		}
		return m, nil

	case PhaseDetailMsg:
		for i := range m.Phases {
			if m.Phases[i].Phase == msg.Phase {
				m.Phases[i].Details = append(m.Phases[i].Details, msg.Message)
				break
			}
		}
		return m, nil

	case ProgressTickMsg:
		m.CompletedOps = msg.Completed
		m.TotalOps = msg.Total
		if m.TotalOps > 0 {
			cmd := m.Progress.SetPercent(float64(m.CompletedOps) / float64(m.TotalOps))
			return m, cmd
		}
		return m, nil

	case CompleteMsg:
		m.Done = true
		m.Summary = msg.Summary
		// Mark all phases as done
		for i := range m.Phases {
			if m.Phases[i].Status == StatusActive {
				m.Phases[i].Status = StatusDone
			}
		}
		return m, tea.Quit
	}

	return m, nil
}

// View renders the model
func (m *Model) View() string {
	var b strings.Builder

	// Progress bar at top (only when active and not done)
	if !m.Done && m.TotalOps > 0 {
		b.WriteString(m.Progress.View())
		b.WriteString(fmt.Sprintf(" %d/%d\n\n", m.CompletedOps, m.TotalOps))
	}

	// Phase headers with their details
	firstPhase := true
	for _, phase := range m.Phases {
		if phase.Status == StatusPending {
			continue // Don't show pending phases
		}

		// Add blank line between phases (not before first)
		if !firstPhase {
			b.WriteString("\n")
		}
		firstPhase = false

		// Phase header
		icon := "✓"
		phaseStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
		if phase.Status == StatusActive {
			icon = m.Spinner.View()
			phaseStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))
		}

		b.WriteString(fmt.Sprintf("%s %s\n", icon, phaseStyle.Render(phase.Message)))

		// Phase details
		dimStyle := style.SubtleStyle()
		for _, detail := range phase.Details {
			b.WriteString(fmt.Sprintf("  %s\n", dimStyle.Render(detail)))
		}
	}

	// Summary
	if m.Done && m.Summary != "" {
		b.WriteString("\n")
		b.WriteString(m.Summary)
		b.WriteString("\n")
	}

	return b.String()
}
