// Package sync provides a TUI component for displaying sync progress.
package sync

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/progress"
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/getstackit/stackit/internal/tui/core"
	"github.com/getstackit/stackit/internal/tui/style"
)

// Phase represents a sync phase
type Phase string

// Phase constants
const (
	PhaseTrunk    Phase = "trunk"
	PhaseBranches Phase = "branches"
	PhaseGitHub   Phase = "github"
	PhaseClean    Phase = "clean"
	PhaseRestack  Phase = "restack"
)

var (
	checkMark = lipgloss.NewStyle().Foreground(lipgloss.Color(style.ColorSuccess)).SetString("✓")
	warnMark  = lipgloss.NewStyle().Foreground(lipgloss.Color(style.ColorWarning)).SetString("⚠")
)

// Model is the bubbletea model for sync progress.
// It embeds core.BaseModel for standard lifecycle handling.
// Uses tea.Printf to print completed items above the active UI.
type Model struct {
	core.BaseModel // Embedded for ReadySignaler interface
	CurrentPhase   Phase
	CurrentDetail  string // Current operation being performed
	TotalOps       int
	CompletedOps   int
	Progress       progress.Model
	spinner        spinner.Model
	Summary        string

	// Phase headers commit to scrollback lazily: only when a phase emits its
	// first detail. Phases that do nothing (nothing to sync/clean/restack) never
	// print an empty header. CurrentPhase still updates eagerly so the live
	// spinner reflects what's happening during slow phases.
	phaseHeaders       map[Phase]string
	committedPhases    map[Phase]bool
	anyHeaderCommitted bool
}

// PhaseStartMsg indicates a phase has started
type PhaseStartMsg struct {
	Phase   Phase
	Message string // Phase header message (e.g., "📥 Pulling from remote...")
}

// PhaseCompleteMsg indicates a phase has completed
type PhaseCompleteMsg struct {
	Phase Phase
}

// PhaseDetailMsg adds a detail line to a phase (printed above TUI)
type PhaseDetailMsg struct {
	Phase   Phase
	Message string
	IsWarn  bool // If true, shows ⚠ instead of ✓
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
		progress.WithDefaultBlend(),
		progress.WithWidth(40),
		progress.WithoutPercentage(),
	)

	commonStyles := style.DefaultCommonStyles()
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = commonStyles.Spinner

	m := &Model{
		TotalOps:        totalOps,
		Progress:        p,
		spinner:         s,
		phaseHeaders:    make(map[Phase]string),
		committedPhases: make(map[Phase]bool),
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
		// Mark the phase active (drives the live spinner) and remember its
		// header, but don't commit it to scrollback yet — the header prints
		// lazily when the phase emits its first detail, so empty phases stay
		// silent.
		m.CurrentPhase = msg.Phase
		m.CurrentDetail = ""
		m.phaseHeaders[msg.Phase] = msg.Message
		return m, nil

	case PhaseCompleteMsg:
		// Phase completed - nothing to do, next phase will start
		return m, nil

	case PhaseDetailMsg:
		// Print completed item above the TUI (package-manager pattern)
		m.CurrentDetail = msg.Message
		mark := checkMark.String()
		if msg.IsWarn {
			mark = warnMark.String()
		}
		detail := tea.Printf("  %s %s", mark, msg.Message)

		// Commit the phase header the first time the phase produces a detail,
		// separated from the previous phase group by a blank line. A detail for
		// a phase that was never started (e.g. standalone restack) just prints
		// without a header.
		if header, started := m.phaseHeaders[msg.Phase]; started && !m.committedPhases[msg.Phase] {
			m.committedPhases[msg.Phase] = true
			var cmds []tea.Cmd
			if m.anyHeaderCommitted {
				cmds = append(cmds, tea.Printf(""))
			}
			m.anyHeaderCommitted = true
			cmds = append(cmds, tea.Printf("%s", header), detail)
			return m, tea.Sequence(cmds...)
		}
		return m, detail

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
		// Print summary and quit
		return m, tea.Sequence(
			tea.Printf("\n%s", msg.Summary),
			tea.Quit,
		)
	}

	return m, nil
}

// View renders the model - shows only the active progress (package-manager pattern)
// Completed items are printed above via tea.Printf
func (m *Model) View() tea.View {
	if m.Done {
		// Summary already printed via tea.Printf in CompleteMsg
		return tea.NewView("")
	}

	var b strings.Builder

	// Progress bar with count (single line, like package-manager)
	n := m.TotalOps
	w := lipgloss.Width(fmt.Sprintf("%d", n))
	pkgCount := fmt.Sprintf(" %*d/%*d", w, m.CompletedOps, w, n)

	spin := m.spinner.View() + " "
	prog := m.Progress.View()

	// Calculate available space for status text
	cellsAvail := max(0, m.Width-lipgloss.Width(spin+prog+pkgCount))

	// Show current phase/operation
	statusText := m.getStatusText()
	info := lipgloss.NewStyle().MaxWidth(cellsAvail).Render(statusText)

	// Fill remaining space
	cellsRemaining := max(0, m.Width-lipgloss.Width(spin+info+prog+pkgCount))
	gap := strings.Repeat(" ", cellsRemaining)

	b.WriteString(spin + info + gap + prog + pkgCount)

	return tea.NewView(b.String())
}

// getStatusText returns the current status text to display
func (m *Model) getStatusText() string {
	commonStyles := style.DefaultCommonStyles()

	switch m.CurrentPhase {
	case PhaseTrunk:
		return "Pulling from remote..."
	case PhaseBranches:
		return "Syncing branches..."
	case PhaseGitHub:
		return "Fetching PR info..."
	case PhaseClean:
		return "Cleaning branches..."
	case PhaseRestack:
		if m.CurrentDetail != "" {
			return commonStyles.Dim.Render("Restacking...")
		}
		return "Restacking branches..."
	default:
		return "Syncing..."
	}
}
