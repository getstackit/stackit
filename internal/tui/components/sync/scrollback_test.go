package sync

import (
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

// printBody extracts the text of a tea.Printf message. Bubble Tea keeps the
// message type unexported, so read it reflectively; ok is false for any other
// message.
func printBody(msg tea.Msg) (string, bool) {
	rv := reflect.ValueOf(msg)
	if !rv.IsValid() || rv.Kind() != reflect.Struct || rv.Type().Name() != "printLineMessage" {
		return "", false
	}
	return rv.FieldByName("messageBody").String(), true
}

// sequenceCmds unpacks tea.Sequence's unexported message ([]tea.Cmd).
func sequenceCmds(msg tea.Msg) ([]tea.Cmd, bool) {
	rv := reflect.ValueOf(msg)
	if !rv.IsValid() || rv.Kind() != reflect.Slice || rv.Type().Elem() != reflect.TypeFor[tea.Cmd]() {
		return nil, false
	}
	if _, isBatch := msg.(tea.BatchMsg); isBatch {
		return nil, false
	}
	cmds := make([]tea.Cmd, rv.Len())
	for i := range cmds {
		cmds[i], _ = rv.Index(i).Interface().(tea.Cmd)
	}
	return cmds, true
}

// scrollbackHarness drives the model like a Bubble Tea program, but executes
// outstanding commands newest-first: the worst-case schedule for commands that
// the real runtime runs concurrently. Sequences still run in order, as in the
// real runtime. Prints are recorded line by line; a print after quit fails the
// test because the real program would drop it.
type scrollbackHarness struct {
	t       *testing.T
	m       *Model
	cmds    []tea.Cmd
	printed []string
	quit    bool
}

func newScrollbackHarness(t *testing.T) *scrollbackHarness {
	t.Helper()
	return &scrollbackHarness{t: t, m: NewModel()}
}

func (h *scrollbackHarness) send(msg tea.Msg) {
	h.t.Helper()
	_, cmd := h.m.Update(msg)
	if cmd != nil {
		h.cmds = append(h.cmds, cmd)
	}
}

// drain runs commands until none are outstanding.
func (h *scrollbackHarness) drain() {
	h.t.Helper()
	for len(h.cmds) > 0 {
		cmd := h.cmds[len(h.cmds)-1]
		h.cmds = h.cmds[:len(h.cmds)-1]
		h.run(cmd)
	}
}

func (h *scrollbackHarness) run(cmd tea.Cmd) {
	h.t.Helper()
	if cmd == nil {
		return
	}
	msg := cmd()
	if body, ok := printBody(msg); ok {
		require.False(h.t, h.quit, "printed %q after quit; the real program would drop it", body)
		h.printed = append(h.printed, strings.Split(body, "\n")...)
		return
	}
	if cmds, ok := sequenceCmds(msg); ok {
		for _, c := range cmds {
			h.run(c)
		}
		return
	}
	switch msg := msg.(type) {
	case nil:
	case tea.QuitMsg:
		h.quit = true
	case tea.BatchMsg:
		h.cmds = append(h.cmds, msg...)
	default:
		if !h.quit {
			h.send(msg)
		}
	}
}

func (h *scrollbackHarness) plain() []string {
	lines := make([]string, len(h.printed))
	for i, line := range h.printed {
		lines[i] = ansi.Strip(line)
	}
	return lines
}

func TestScrollback_RowsPrintInOrderUnderWorstCaseScheduling(t *testing.T) {
	t.Parallel()
	h := newScrollbackHarness(t)
	h.send(PhaseStartMsg{Phase: PhaseTrunk, Message: "TRUNK"})
	h.send(PhaseStartMsg{Phase: PhaseGitHub, Message: "GITHUB"})

	var want []string
	add := func(phase Phase, mark DetailMark, text string) {
		h.send(PhaseDetailMsg{Phase: phase, Mark: mark, Message: text})
	}
	// Rows arrive faster than prints complete: several queue behind each print.
	add(PhaseTrunk, MarkDone, "main fast-forwarded")
	add(PhaseGitHub, MarkDone, "Updated PR info for 3 branches")
	want = append(want, "TRUNK", "  ✓ main fast-forwarded", "", "GITHUB", "  ✓ Updated PR info for 3 branches")
	h.send(PhaseStartMsg{Phase: PhaseRestack, Message: "RESTACK"})
	for i := range 50 {
		add(PhaseRestack, MarkDone, fmt.Sprintf("Restacked b%02d", i))
		if i == 0 {
			want = append(want, "", "RESTACK")
		}
		want = append(want, fmt.Sprintf("  ✓ Restacked b%02d", i))
		if i%7 == 0 {
			h.drain()
		}
	}
	add(PhaseRestack, MarkWarn, "Held feat/api back: dirty worktree")
	want = append(want, "  ⚠ Held feat/api back: dirty worktree")
	h.send(CompleteMsg{Summary: "SUMMARY"})
	want = append(want, "", "SUMMARY")
	h.drain()

	require.True(t, h.quit, "model must quit once scrollback drains")
	require.Equal(t, want, h.plain())
}

func TestScrollback_CompleteWaitsForInFlightPrint(t *testing.T) {
	t.Parallel()
	h := newScrollbackHarness(t)
	h.send(PhaseDetailMsg{Phase: PhaseRestack, Mark: MarkWarn, Message: "Held feat/api back: reason"})
	// Complete arrives before the row's print has run.
	h.send(CompleteMsg{Summary: "1 branch held"})
	require.False(t, h.quit)
	h.drain()

	require.True(t, h.quit)
	require.Equal(t, []string{"  ⚠ Held feat/api back: reason", "", "1 branch held"}, h.plain())
}

func TestScrollback_EmptySummaryQuitsWithoutPrinting(t *testing.T) {
	t.Parallel()
	h := newScrollbackHarness(t)
	h.send(CompleteMsg{})
	h.drain()

	require.True(t, h.quit)
	require.Empty(t, h.printed)
}

func TestScrollback_InProgressRowsNeverPrint(t *testing.T) {
	t.Parallel()
	h := newScrollbackHarness(t)
	h.send(PhaseStartMsg{Phase: PhaseGitHub, Message: "GITHUB"})
	h.send(PhaseDetailMsg{Phase: PhaseGitHub, Mark: MarkInProgress, Message: "Updating PR metadata for 2 branches..."})
	h.send(CompleteMsg{Summary: "done"})
	h.drain()

	require.Equal(t, []string{"", "done"}, h.plain())
}

// TestScrollback_RealProgramKeepsEveryRow runs the model inside a real Bubble
// Tea program, where commands genuinely execute concurrently, and records print
// messages in the order the event loop processes them.
func TestScrollback_RealProgramKeepsEveryRow(t *testing.T) {
	t.Parallel()
	const rows = 200

	model := NewModel()
	ready := make(chan struct{})
	model.SetReadyChan(ready)

	var processed []string // only touched by the event loop goroutine
	p := tea.NewProgram(model,
		tea.WithInput(nil),
		tea.WithOutput(io.Discard),
		tea.WithWindowSize(80, 24),
		tea.WithoutSignals(),
		tea.WithFilter(func(_ tea.Model, msg tea.Msg) tea.Msg {
			if body, ok := printBody(msg); ok {
				processed = append(processed, strings.Split(ansi.Strip(body), "\n")...)
			}
			return msg
		}),
	)
	done := make(chan error, 1)
	go func() {
		_, err := p.Run()
		done <- err
	}()
	select {
	case <-ready:
	case <-time.After(10 * time.Second):
		t.Fatal("program never became ready")
	}

	want := make([]string, 0, rows+3)
	want = append(want, "RESTACK")
	p.Send(PhaseStartMsg{Phase: PhaseRestack, Message: "RESTACK"})
	for i := range rows {
		p.Send(PhaseDetailMsg{Phase: PhaseRestack, Message: fmt.Sprintf("row %03d", i)})
		want = append(want, fmt.Sprintf("  ✓ row %03d", i))
	}
	p.Send(CompleteMsg{Summary: "SUMMARY"})
	want = append(want, "", "SUMMARY")

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(10 * time.Second):
		p.Kill()
		t.Fatal("program did not quit after CompleteMsg")
	}
	require.Equal(t, want, processed)
}

func TestModel_ElapsedAdvancesOnSpinnerTick(t *testing.T) {
	t.Parallel()
	m := NewModel()
	m.started = time.Now().Add(-3 * time.Second)

	m.Update(spinner.TickMsg{})

	require.GreaterOrEqual(t, m.elapsed, 3*time.Second)
	require.Contains(t, ansi.Strip(m.View().Content), "3.", "elapsed seconds should show in the live line")
}

func TestModel_ElapsedWithoutInitStartsAtConstruction(t *testing.T) {
	t.Parallel()
	m := NewModel()

	m.Update(spinner.TickMsg{})

	require.Less(t, m.elapsed, time.Minute, "a model driven without Init must not measure from the zero time")
}

func TestModel_ViewShowsCountOnlyWithTotal(t *testing.T) {
	t.Parallel()
	m := NewModel()
	m.Update(PhaseStartMsg{Phase: PhaseRestack, Message: "RESTACK"})
	require.NotContains(t, ansi.Strip(m.View().Content), "/", "no k/N suffix without a known total")

	m.Update(ProgressTickMsg{Completed: 2, Total: 5})
	require.Contains(t, ansi.Strip(m.View().Content), "2/5")
}
