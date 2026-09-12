// Package sync provides a TUI component for displaying sync progress.
package sync

import (
	"fmt"
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
//
// Completed items print above the active UI. Bubble Tea runs commands
// concurrently, so independent tea.Printf commands can land in any order — or
// after a tea.Quit returned alongside them. The model therefore owns a FIFO of
// scrollback lines and keeps at most one print in flight: each print is chained
// with a printedMsg via tea.Sequence, and the next batch (everything queued
// meanwhile, coalesced into one print) is flushed only when that message
// arrives. CompleteMsg queues the summary behind the pending lines and quits
// only once the queue has drained, so no row is reordered or lost.
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

	// Phase headers commit to scrollback lazily: only when a phase emits its
	// first detail. Phases that do nothing (nothing to sync/clean/restack) never
	// print an empty header. CurrentPhase still updates eagerly so the live
	// spinner reflects what's happening during slow phases.
	//
	// phaseHeaders holds each phase's pending header text; headers tracks the
	// commit decision (the same utils.LazyHeaders rule the streaming handler uses).
	phaseHeaders map[Phase]string
	headers      *utils.LazyHeaders[Phase]

	// Ordered scrollback: lines waiting to print, whether a print is in
	// flight, and whether to quit once everything has printed.
	pending     []string
	printing    bool
	quitOnDrain bool
}

// printedMsg reports that the in-flight scrollback print has been processed by
// the program, so the next batch may be flushed.
type printedMsg struct{}

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

// ProgressTickMsg updates the k/N counter shown beside the elapsed time when a
// phase knows its exact item count (e.g. standalone restack).
type ProgressTickMsg struct {
	Completed int
	Total     int
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

	m := &Model{
		spinner:      s,
		started:      time.Now(),
		phaseHeaders: make(map[Phase]string),
		headers:      utils.NewLazyHeaders[Phase](),
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

		// Commit the phase header the first time the phase produces a detail,
		// separated from the previous phase group by a blank line. A detail for
		// a phase that was never started (e.g. standalone restack) just prints
		// without a header. The ordered queue keeps the header above its rows.
		if emit, separate := m.headers.CommitOnItem(msg.Phase); emit {
			if separate {
				m.pending = append(m.pending, "")
			}
			m.pending = append(m.pending, m.phaseHeaders[msg.Phase])
		}
		m.pending = append(m.pending, fmt.Sprintf("  %s %s", msg.Mark.glyph(), msg.Message))
		return m, m.flush()

	case printedMsg:
		m.printing = false
		return m, m.flush()

	case ProgressTickMsg:
		m.CompletedOps = msg.Completed
		m.TotalOps = msg.Total
		return m, nil

	case CompleteMsg:
		m.Done = true
		m.Summary = msg.Summary
		m.quitOnDrain = true
		// The summary prints after every queued row; flush quits once drained.
		if msg.Summary != "" {
			m.pending = append(m.pending, "", msg.Summary)
		}
		return m, m.flush()
	}

	return m, nil
}

// flush starts printing everything queued unless a print is already in flight;
// that print's printedMsg flushes whatever accumulated meanwhile. Once the
// queue is empty and nothing is printing, a completed model quits — never
// earlier, so Quit cannot overtake a pending row.
func (m *Model) flush() tea.Cmd {
	if m.printing {
		return nil
	}
	if len(m.pending) == 0 {
		if m.quitOnDrain {
			return tea.Quit
		}
		return nil
	}
	batch := strings.Join(m.pending, "\n")
	m.pending = nil
	m.printing = true
	return tea.Sequence(
		tea.Printf("%s", batch),
		func() tea.Msg { return printedMsg{} },
	)
}

// View renders the model - shows only the active progress (package-manager pattern)
// Completed items are printed above through the ordered scrollback queue.
func (m *Model) View() tea.View {
	if m.Done {
		// The summary prints through the scrollback queue after CompleteMsg.
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
