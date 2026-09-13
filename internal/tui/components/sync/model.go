// Package sync provides a TUI component for displaying sync progress.
package sync

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/getstackit/stackit/internal/tui/core"
	"github.com/getstackit/stackit/internal/tui/style"
	"github.com/getstackit/stackit/internal/utils"
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

// DetailMark selects the status glyph shown on a detail row. Using a typed
// constant (rather than a bare bool) keeps the three states distinct at the call
// site and matches the streaming handler's markers.
type DetailMark int

const (
	// MarkDone shows a green ✓ for a completed item.
	MarkDone DetailMark = iota
	// MarkWarn shows an orange ⚠ for a skipped/attention item.
	MarkWarn
	// MarkInProgress shows a dim → for an item still in flight.
	MarkInProgress
)

// glyph returns the colored marker string for this status.
func (d DetailMark) glyph() string {
	switch d {
	case MarkWarn:
		return style.MarkWarning()
	case MarkInProgress:
		return style.MarkProgress()
	default:
		return style.MarkSuccess()
	}
}

// Model is the bubbletea model for sync progress.
// It embeds core.BaseModel for standard lifecycle handling.
// Uses tea.Printf to print completed items above the active UI.
type Model struct {
	core.BaseModel // Embedded for ReadySignaler interface
	CurrentPhase   Phase
	CurrentDetail  string // Current operation being performed
	TotalOps       int
	CompletedOps   int
	started        time.Time
	elapsed        time.Duration
	spinner        spinner.Model
	Summary        string
	active         map[string]string

	// Phase headers commit to scrollback lazily: only when a phase emits its
	// first detail. Phases that do nothing (nothing to sync/clean/restack) never
	// print an empty header. CurrentPhase still updates eagerly so the live
	// spinner reflects what's happening during slow phases.
	//
	// phaseHeaders holds each phase's pending header text; headers tracks the
	// commit decision (the same utils.LazyHeaders rule the streaming handler uses).
	phaseHeaders map[Phase]string
	headers      *utils.LazyHeaders[Phase]
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
	Mark    DetailMark // Status glyph for the row (defaults to MarkDone)
}

// ActivityMsg names an in-flight rebase check; several may run concurrently.
type ActivityMsg struct {
	Branch   string
	Parent   string
	Finished bool
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
	commonStyles := style.DefaultCommonStyles()
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = commonStyles.Spinner

	m := &Model{
		TotalOps:     totalOps,
		spinner:      s,
		phaseHeaders: make(map[Phase]string),
		headers:      utils.NewLazyHeaders[Phase](),
	}
	m.Width = 80 // Set BaseModel's Width
	return m
}

// Init initializes the model
func (m *Model) Init() tea.Cmd {
	// Signal that the program is ready to receive messages via BaseModel
	m.started = time.Now()
	m.SignalReady()
	return m.spinner.Tick
}

// Update handles messages
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Handle spinner ticks with our custom spinner BEFORE HandleCommonMsg
	// (HandleCommonMsg would update BaseModel.Spinner instead)
	if tickMsg, ok := msg.(spinner.TickMsg); ok {
		m.elapsed = time.Since(m.started)
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
		// Mark the phase active (drives the live spinner) and remember its
		// header, but don't commit it to scrollback yet — the header prints
		// lazily when the phase emits its first detail, so empty phases stay
		// silent.
		m.CurrentPhase = msg.Phase
		m.CurrentDetail = ""
		m.phaseHeaders[msg.Phase] = msg.Message
		m.headers.Start(msg.Phase)
		return m, nil

	case PhaseCompleteMsg:
		// Phase completed - nothing to do, next phase will start
		return m, nil

	case PhaseDetailMsg:
		// In-flight details belong only in the live status line.
		if msg.Mark == MarkInProgress {
			m.CurrentDetail = msg.Message
			return m, nil
		}
		m.CurrentDetail = ""
		detail := fmt.Sprintf("  %s %s", msg.Mark.glyph(), msg.Message)

		// Commit the phase header the first time the phase produces a detail,
		// separated from the previous phase group by a blank line. A detail for
		// a phase that was never started (e.g. standalone restack) just prints
		// without a header.
		if emit, separate := m.headers.CommitOnItem(msg.Phase); emit {
			var lines []string
			if separate {
				lines = append(lines, "")
			}
			// Keep the header and its first row in one print message. Separate
			// commands can interleave with a subsequent branch's result.
			lines = append(lines, m.phaseHeaders[msg.Phase], detail)
			return m, tea.Printf("%s", strings.Join(lines, "\n"))
		}
		return m, tea.Printf("%s", detail)

	case ActivityMsg:
		if m.active == nil {
			m.active = make(map[string]string)
		}
		if msg.Finished {
			delete(m.active, msg.Branch)
		} else {
			m.active[msg.Branch] = msg.Parent
		}
		return m, nil

	case ProgressTickMsg:
		m.CompletedOps = msg.Completed
		m.TotalOps = msg.Total
		return m, nil

	case CompleteMsg:
		m.Done = true
		m.Summary = msg.Summary
		if msg.Summary == "" {
			return m, tea.Quit
		}
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

	status := m.getStatusText()
	suffix := fmt.Sprintf("  %.1fs", m.elapsed.Seconds())
	if m.TotalOps > 0 {
		suffix = fmt.Sprintf("  %d/%d", m.CompletedOps, m.TotalOps) + suffix
	}
	spin := m.spinner.View() + " "
	width := max(1, m.Width)
	available := width - lipgloss.Width(spin+suffix)
	if available < 12 {
		// Activity is more useful than counters in a narrow terminal.
		suffix = ""
		available = max(0, width-lipgloss.Width(spin))
	}
	status = ansi.Truncate(status, available, "…")
	gap := strings.Repeat(" ", max(0, width-lipgloss.Width(spin+status+suffix)))
	return tea.NewView(ansi.Truncate(spin+status+gap+suffix, width, "…"))
}

// getStatusText returns the current status text to display
func (m *Model) getStatusText() string {
	if len(m.active) > 0 {
		names := make([]string, 0, len(m.active))
		for name := range m.active {
			names = append(names, name)
		}
		sort.Strings(names)
		text := "Checking rebase: " + style.DisplayBranchName(names[0]) + " onto " + style.DisplayBranchName(m.active[names[0]])
		if len(names) > 1 {
			text += fmt.Sprintf(" (+%d active)", len(names)-1)
		}
		return text
	}
	if m.CurrentDetail != "" {
		return m.CurrentDetail
	}
	switch m.CurrentPhase {
	case PhaseTrunk:
		return "Pulling from remote..."
	case PhaseBranches:
		return "Syncing branches..."
	case PhaseGitHub:
		return "Fetching remote branches and PR status..."
	case PhaseClean:
		return "Cleaning branches..."
	case PhaseRestack:
		return "Restacking branches..."
	default:
		return "Syncing..."
	}
}
