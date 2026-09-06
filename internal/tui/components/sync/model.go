// Package sync provides a TUI component for displaying sync progress.
package sync

import (
	"fmt"
	"strings"
	"time"

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

// Model is the bubbletea model for sync progress.
// It embeds core.BaseModel for standard lifecycle handling.
// Uses tea.Printf to print completed items above the active UI.
type Model struct {
	core.BaseModel // Embedded for ReadySignaler interface
	CurrentPhase   Phase
	CurrentDetail  string // Current operation being performed
	spinner        spinner.Model
	Summary        string

	// started is set on Init and drives the elapsed clock in View.
	started time.Time
}

// PhaseStartMsg names the phase now in flight. It drives the live line only —
// nothing is written to scrollback until the phase is over and its outcome is
// known (see the sync handler's report).
type PhaseStartMsg struct {
	Phase Phase
}

// ActivityMsg names the specific work in flight, replacing the phase label on
// the live line until the phase moves on. It is never written to scrollback: it
// is superseded by whatever the phase reports at the end.
type ActivityMsg struct {
	Text string
}

// PrintMsg writes finished lines above the live UI. The handler has already
// composed and styled them, so the model does not decide what a row looks like.
type PrintMsg struct {
	Lines []string
}

// CompleteMsg indicates sync is complete
type CompleteMsg struct {
	Summary string
}

// NewModel creates a new sync model
func NewModel() *Model {
	commonStyles := style.DefaultCommonStyles()
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = commonStyles.Spinner

	m := &Model{spinner: s}
	m.Width = 80 // Set BaseModel's Width
	return m
}

// Init initializes the model
func (m *Model) Init() tea.Cmd {
	// Signal that the program is ready to receive messages via BaseModel
	m.SignalReady()
	m.started = time.Now()
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
	case PhaseStartMsg:
		m.CurrentPhase = msg.Phase
		m.CurrentDetail = ""
		return m, nil

	case ActivityMsg:
		m.CurrentDetail = msg.Text
		return m, nil

	case PrintMsg:
		// One phase's finished output, printed above the live UI in one burst.
		cmds := make([]tea.Cmd, 0, len(msg.Lines))
		for _, line := range msg.Lines {
			cmds = append(cmds, tea.Printf("%s", line))
		}
		if len(cmds) == 0 {
			return m, nil
		}
		return m, tea.Sequence(cmds...)

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

	spin := m.spinner.View() + " "
	elapsed := style.DefaultCommonStyles().Dim.Render(m.elapsedText())

	// Calculate available space for status text
	cellsAvail := max(0, m.Width-lipgloss.Width(spin+elapsed))

	// Show the phase step and what it's doing
	info := lipgloss.NewStyle().MaxWidth(cellsAvail).Render(m.getStatusText())

	// Fill remaining space
	cellsRemaining := max(0, m.Width-lipgloss.Width(spin+info+elapsed))
	gap := strings.Repeat(" ", cellsRemaining)

	b.WriteString(spin + info + gap + elapsed)

	return tea.NewView(b.String())
}

// elapsedText renders how long the command has been running. Sync is the
// command users actually wait on — network fetch, GitHub, then a rebase
// validation pass that emits nothing until it finishes — so a moving clock is
// the difference between "working" and "hung". Redraws come free from the
// spinner tick.
func (m *Model) elapsedText() string {
	if m.started.IsZero() {
		return ""
	}
	return fmt.Sprintf("%.1fs", time.Since(m.started).Seconds())
}

// phaseOrder is the fixed pipeline sync walks, and the source of the step
// numbers on the live line. Trunk and GitHub run concurrently but are still
// numbered separately, so the step reflects position in the pipeline rather
// than a claim about what is exclusively in flight.
//
// Progress is reported as a step in this pipeline rather than as a fraction of
// items. Sync's item count is not knowable up front — it depends on the restack
// plan, the deletion plan, and how many PR bodies are stale — and the previous
// bar counted events against an estimate of the tracked branch count, two
// different units, which is how it reached "11/10". The pipeline, by contrast,
// is known before anything runs.
var phaseOrder = []Phase{PhaseTrunk, PhaseGitHub, PhaseBranches, PhaseClean, PhaseRestack}

// phaseLabels is what each phase is called on the live line.
var phaseLabels = map[Phase]string{
	PhaseTrunk:    "Pulling from remote...",
	PhaseGitHub:   "Fetching PR info...",
	PhaseBranches: "Syncing branches...",
	PhaseClean:    "Cleaning branches...",
	PhaseRestack:  "Restacking branches...",
}

// getStatusText returns the current status text: the pipeline position as pips
// followed by what that phase is doing (e.g. "●●●●○ Restacking branches...").
// A skipped phase simply never lights its pip; the pips are a position, not a
// count of completed work, so they stay truthful when phases are skipped.
//
// When a phase reports in-flight work it names that instead of the phase, so
// the line says what is happening now rather than which phase it belongs to.
func (m *Model) getStatusText() string {
	label, ok := phaseLabels[m.CurrentPhase]
	if !ok {
		return "Syncing..."
	}
	if m.CurrentDetail != "" {
		label = m.CurrentDetail
	}
	for i, phase := range phaseOrder {
		if phase == m.CurrentPhase {
			return style.StepPips(i+1, len(phaseOrder)) + " " + label
		}
	}
	return label
}
