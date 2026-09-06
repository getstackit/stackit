package stack

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	syncAction "github.com/getstackit/stackit/internal/actions/sync"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/handlers"
	"github.com/getstackit/stackit/internal/output"
	"github.com/getstackit/stackit/internal/tui"
	syncComponent "github.com/getstackit/stackit/internal/tui/components/sync"
)

func TestInteractiveSyncHandler_Start(t *testing.T) {
	mockRunner := tui.NewMockRunner()
	model := syncComponent.NewModel()
	handler := NewInteractiveSyncHandler(mockRunner, model, output.NewNullOutput(), output.NewNullLogger())

	handler.Start(10)

	// Start seeds nothing. Its argument was a run-level item estimate that drove
	// a progress bar; the live line now shows the phase's position in a pipeline
	// that is known without any estimate.
	assert.Empty(t, mockRunner.Messages())
}

func TestInteractiveSyncHandler_EmitEvent_PhaseStart(t *testing.T) {
	mockRunner := tui.NewMockRunner()
	model := syncComponent.NewModel()
	handler := NewInteractiveSyncHandler(mockRunner, model, output.NewNullOutput(), output.NewNullLogger())

	// Emit a phase start event
	handler.EmitEvent(syncAction.Event{
		Phase: syncAction.PhaseTrunk,
		Type:  syncAction.EventStarted,
	})

	// Verify that a PhaseStartMsg was sent
	messages := mockRunner.Messages()
	require.Len(t, messages, 1)

	msg, ok := messages[0].(syncComponent.PhaseStartMsg)
	require.True(t, ok, "expected PhaseStartMsg, got %T", messages[0])
	assert.Equal(t, syncComponent.PhaseTrunk, msg.Phase)
}

func TestInteractiveSyncHandler_EmitEvent_Progress(t *testing.T) {
	mockRunner := tui.NewMockRunner()
	model := syncComponent.NewModel()
	handler := NewInteractiveSyncHandler(mockRunner, model, output.NewNullOutput(), output.NewNullLogger())

	// Set up initial state
	handler.Start(5)
	mockRunner.Reset() // Clear the Start message

	// Set current phase to trunk
	handler.EmitEvent(syncAction.Event{
		Phase: syncAction.PhaseTrunk,
		Type:  syncAction.EventStarted,
	})
	mockRunner.Reset() // Clear the phase start message

	// Emit a completed event
	handler.EmitEvent(syncAction.Event{
		Phase:       syncAction.PhaseTrunk,
		Type:        syncAction.EventCompleted,
		Branch:      "main",
		NewRevision: "abc1234",
		Commits:     3,
	})

	// Nothing is printed yet: a phase's outcome header can only be written once
	// the phase is over, so its rows are still buffered in the report.
	assert.Empty(t, mockRunner.Messages())

	// Ending the run flushes the trunk phase, which reports that trunk moved.
	handler.Complete(syncAction.Summary{TrunkUpdated: true, TrunkRevision: "abc1234", TrunkCommits: 3})
	printed := printedLines(mockRunner.Messages())
	assert.Contains(t, printed, "main")
	assert.Contains(t, printed, "+3")
}

// printedLines concatenates every line the handler sent for printing, with color
// stripped, for assertions about what reached the transcript.
func printedLines(messages []tea.Msg) string {
	var b strings.Builder
	for _, msg := range messages {
		if print, ok := msg.(syncComponent.PrintMsg); ok {
			for _, line := range print.Lines {
				b.WriteString(ansi.Strip(line))
				b.WriteString("\n")
			}
		}
	}
	return b.String()
}

func TestInteractiveSyncHandler_Complete(t *testing.T) {
	mockRunner := tui.NewMockRunner()
	model := syncComponent.NewModel()
	handler := NewInteractiveSyncHandler(mockRunner, model, output.NewNullOutput(), output.NewNullLogger())

	handler.Complete(syncAction.Summary{
		UpToDate: true,
	})

	// Verify that Complete sends a CompleteMsg
	messages := mockRunner.Messages()
	require.Len(t, messages, 1)

	msg, ok := messages[0].(syncComponent.CompleteMsg)
	require.True(t, ok, "expected CompleteMsg, got %T", messages[0])
	assert.Contains(t, msg.Summary, "up to date")
}

func TestInteractiveSyncHandler_OnRestackStart(t *testing.T) {
	mockRunner := tui.NewMockRunner()
	model := syncComponent.NewModel()
	handler := NewInteractiveSyncHandler(mockRunner, model, output.NewNullOutput(), output.NewNullLogger())

	handler.OnRestackStart(3)

	// One PhaseStartMsg. The branch count no longer feeds a bar, so it isn't
	// forwarded to the model — the live line shows the phase's pipeline position.
	messages := mockRunner.Messages()
	require.Len(t, messages, 1)

	phaseMsg, ok := messages[0].(syncComponent.PhaseStartMsg)
	require.True(t, ok, "expected PhaseStartMsg, got %T", messages[0])
	assert.Equal(t, syncComponent.PhaseRestack, phaseMsg.Phase)
}

func TestInteractiveSyncHandler_OnRestackBranch(t *testing.T) {
	mockRunner := tui.NewMockRunner()
	model := syncComponent.NewModel()
	handler := NewInteractiveSyncHandler(mockRunner, model, output.NewNullOutput(), output.NewNullLogger())

	// Set up initial state
	handler.OnRestackStart(2)
	mockRunner.Reset() // Clear setup messages

	// Simulate restacking a branch
	prNumber := 42
	handler.OnRestackBranch(handlers.RestackBranchEvent{
		Branch:      "feature-branch",
		Result:      syncAction.RestackDone,
		NewRevision: "def5678",
		PRNumber:    &prNumber,
		LockReason:  engine.LockReasonNone,
		IsCurrent:   true,
		Parent:      "main",
	})

	// A plain successful restack is what the developer assumed would happen, so
	// it contributes a count rather than a row.
	handler.OnRestackComplete(handlers.RestackSummary{Restacked: 1})
	assert.Contains(t, printedLines(mockRunner.Messages()), "Restacked 1 branch")
}

func TestInteractiveSyncHandler_ReportsReparenting(t *testing.T) {
	mockRunner := tui.NewMockRunner()
	model := syncComponent.NewModel()
	handler := NewInteractiveSyncHandler(mockRunner, model, output.NewNullOutput(), output.NewNullLogger())

	handler.OnRestackStart(1)
	mockRunner.Reset()

	// A reparent is the one sync outcome a developer cannot infer: the shape of
	// their stack changed because the old parent landed. It must be reported.
	handler.OnRestackBranch(handlers.RestackBranchEvent{
		Branch:      "child",
		Result:      syncAction.RestackDone,
		NewRevision: "def5678",
		LockReason:  engine.LockReasonNone,
		Parent:      "main",
		Reparented:  true,
		OldParent:   "landed-parent",
		NewParent:   "main",
	})
	handler.OnRestackComplete(handlers.RestackSummary{Restacked: 1})

	printed := printedLines(mockRunner.Messages())
	assert.Contains(t, printed, "child")
	assert.Contains(t, printed, "reparented onto main")
	assert.Contains(t, printed, "landed-parent")
}

func TestInteractiveSyncHandler_OnRestackComplete(t *testing.T) {
	mockRunner := tui.NewMockRunner()
	model := syncComponent.NewModel()
	handler := NewInteractiveSyncHandler(mockRunner, model, output.NewNullOutput(), output.NewNullLogger())

	handler.OnRestackComplete(handlers.RestackSummary{Restacked: 5, Skipped: 2})

	// Verify that OnRestackComplete sends a CompleteMsg
	messages := mockRunner.Messages()
	require.Len(t, messages, 1)

	msg, ok := messages[0].(syncComponent.CompleteMsg)
	require.True(t, ok, "expected CompleteMsg, got %T", messages[0])
	assert.Contains(t, msg.Summary, "Restacked 5")
	assert.Contains(t, msg.Summary, "skipped 2")
}

func TestInteractiveSyncHandler_IsInteractive(t *testing.T) {
	mockRunner := tui.NewMockRunner()
	model := syncComponent.NewModel()
	handler := NewInteractiveSyncHandler(mockRunner, model, output.NewNullOutput(), output.NewNullLogger())

	assert.True(t, handler.IsInteractive())
}

func TestSimpleSyncHandler_IsNotInteractive(t *testing.T) {
	handler := NewSimpleSyncHandler(output.NewNullOutput())
	assert.False(t, handler.IsInteractive())
}
