// Package flatten provides a TUI component for displaying flatten progress.
package flatten

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/progress"
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"

	"github.com/getstackit/stackit/internal/tui/core"
	"github.com/getstackit/stackit/internal/tui/style"
)

// Phase represents a flatten phase
type Phase string

// Phase constants
const (
	PhaseAnalyzing  Phase = "analyzing"
	PhaseValidating Phase = "validating"
	PhaseFlattening Phase = "flattening"
	PhaseRestacking Phase = "restacking"
)

// PhaseItem represents progress for a single phase
type PhaseItem struct {
	Phase   Phase
	Status  core.Status
	Message string
	Details []string // Lines of detail output for this phase
}

// Model is the bubbletea model for flatten progress.
// It embeds core.BaseModel for standard lifecycle handling.
type Model struct {
	core.BaseModel  // Embedded for ReadySignaler interface
	Phases          []PhaseItem
	CurrentPhase    Phase
	Progress        progress.Model
	spinner         spinner.Model // Use local spinner for custom style
	ValidatingIdx   int
	ValidatingTotal int
	CurrentBranch   string
	Summary         string
}

// ValidationProgressMsg updates validation progress
type ValidationProgressMsg struct {
	Current int
	Total   int
	Branch  string
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

// CompleteMsg indicates flatten is complete
type CompleteMsg struct {
	Summary string
}

// NewModel creates a new flatten model
func NewModel() *Model {
	p := progress.New(
		progress.WithDefaultBlend(),
		progress.WithWidth(40),
		progress.WithoutPercentage(),
	)

	commonStyles := style.DefaultCommonStyles()
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = commonStyles.Spinner

	m := &Model{
		Phases: []PhaseItem{
			{Phase: PhaseAnalyzing, Status: core.StatusPending, Message: "Analyzing stack structure..."},
			{Phase: PhaseValidating, Status: core.StatusPending, Message: "Validating moves..."},
			{Phase: PhaseFlattening, Status: core.StatusPending, Message: "Moving branches..."},
			{Phase: PhaseRestacking, Status: core.StatusPending, Message: "Restacking branches..."},
		},
		Progress: p,
		spinner:  s,
	}
	m.Width = 80 // Set BaseModel's Width
	return m
}

// Init initializes the model
func (m *Model) Init() tea.Cmd {
	// Signal that the program is ready to receive messages via BaseModel
	m.SignalReady()
	return m.spinner.Tick
}

// Update handles messages
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Handle spinner ticks with our custom spinner BEFORE HandleCommonMsg
	// (HandleCommonMsg would update BaseModel.Spinner instead)
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
	case tea.WindowSizeMsg:
		// BaseModel already set Width/Height, but we also need to update Progress.Width
		m.Progress.SetWidth(min(msg.Width-10, 60))
		return m, nil

	case progress.FrameMsg:
		var cmd tea.Cmd
		m.Progress, cmd = m.Progress.Update(msg)
		return m, cmd

	case PhaseStartMsg:
		m.CurrentPhase = msg.Phase
		for i := range m.Phases {
			if m.Phases[i].Phase == msg.Phase {
				m.Phases[i].Status = core.StatusActive
			} else if m.Phases[i].Status == core.StatusActive {
				m.Phases[i].Status = core.StatusDone
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

	case ValidationProgressMsg:
		m.ValidatingIdx = msg.Current
		m.ValidatingTotal = msg.Total
		m.CurrentBranch = msg.Branch
		if m.ValidatingTotal > 0 {
			cmd := m.Progress.SetPercent(float64(m.ValidatingIdx) / float64(m.ValidatingTotal))
			return m, cmd
		}
		return m, nil

	case CompleteMsg:
		m.Done = true
		m.Summary = msg.Summary
		// Mark all phases as done
		for i := range m.Phases {
			if m.Phases[i].Status == core.StatusActive {
				m.Phases[i].Status = core.StatusDone
			}
		}
		return m, tea.Quit
	}

	return m, nil
}

// View renders the model
func (m *Model) View() tea.View {
	var b strings.Builder

	// Get shared styles and icons
	statusStyles := style.DefaultStatusStyles()
	statusIcons := style.DefaultStatusIcons()
	commonStyles := style.DefaultCommonStyles()

	// Progress bar at top (during analyzing or validating phases)
	if !m.Done && (m.CurrentPhase == PhaseAnalyzing || m.CurrentPhase == PhaseValidating) && m.ValidatingTotal > 0 {
		b.WriteString(m.Progress.View())
		fmt.Fprintf(&b, " %d/%d", m.ValidatingIdx, m.ValidatingTotal)
		if m.CurrentBranch != "" {
			fmt.Fprintf(&b, " %s", commonStyles.Subtle.Render(m.CurrentBranch))
		}
		b.WriteString("\n\n")
	}

	// Phase headers with their details
	firstPhase := true
	for _, phase := range m.Phases {
		if phase.Status == core.StatusPending {
			continue // Don't show pending phases
		}

		// Add blank line between phases (not before first)
		if !firstPhase {
			b.WriteString("\n")
		}
		firstPhase = false

		// Phase header
		icon := statusIcons.Done
		phaseStyle := statusStyles.Done
		if phase.Status == core.StatusActive {
			icon = m.spinner.View()
			phaseStyle = statusStyles.Active
		}

		fmt.Fprintf(&b, "%s %s\n", icon, phaseStyle.Render(phase.Message))

		// Phase details
		for _, detail := range phase.Details {
			fmt.Fprintf(&b, "  %s\n", commonStyles.Subtle.Render(detail))
		}
	}

	// Summary
	if m.Done && m.Summary != "" {
		b.WriteString("\n")
		b.WriteString(m.Summary)
		b.WriteString("\n")
	}

	return tea.NewView(b.String())
}
