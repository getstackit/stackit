package stack

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	syncAction "github.com/getstackit/stackit/internal/actions/sync"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/output"
	"github.com/getstackit/stackit/internal/tui"
	syncComponent "github.com/getstackit/stackit/internal/tui/components/sync"
)

func TestInteractiveSyncHandler_Start(t *testing.T) {
	mockRunner := tui.NewMockRunner()
	model := syncComponent.NewModel(0)
	handler := NewInteractiveSyncHandler(mockRunner, model, output.NewNullOutput(), output.NewNullLogger())

	handler.Start(10)

	// Verify that Start sends a ProgressTickMsg
	messages := mockRunner.Messages()
	require.Len(t, messages, 1)

	msg, ok := messages[0].(syncComponent.ProgressTickMsg)
	require.True(t, ok, "expected ProgressTickMsg, got %T", messages[0])
	assert.Equal(t, 0, msg.Completed)
	assert.Equal(t, 10, msg.Total)
}

func TestInteractiveSyncHandler_EmitEvent_PhaseStart(t *testing.T) {
	mockRunner := tui.NewMockRunner()
	model := syncComponent.NewModel(0)
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
	model := syncComponent.NewModel(0)
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
	})

	// Should send PhaseDetailMsg and ProgressTickMsg
	messages := mockRunner.Messages()
	require.Len(t, messages, 2)

	// First message should be PhaseDetailMsg
	detailMsg, ok := messages[0].(syncComponent.PhaseDetailMsg)
	require.True(t, ok, "expected PhaseDetailMsg, got %T", messages[0])
	assert.Equal(t, syncComponent.PhaseTrunk, detailMsg.Phase)
	assert.Contains(t, detailMsg.Message, "main")
	assert.Contains(t, detailMsg.Message, "abc1234")

	// Second message should be ProgressTickMsg
	progressMsg, ok := messages[1].(syncComponent.ProgressTickMsg)
	require.True(t, ok, "expected ProgressTickMsg, got %T", messages[1])
	assert.Equal(t, 1, progressMsg.Completed)
	assert.Equal(t, 5, progressMsg.Total)
}

func TestInteractiveSyncHandler_Complete(t *testing.T) {
	mockRunner := tui.NewMockRunner()
	model := syncComponent.NewModel(0)
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
	model := syncComponent.NewModel(0)
	handler := NewInteractiveSyncHandler(mockRunner, model, output.NewNullOutput(), output.NewNullLogger())

	handler.OnRestackStart(3)

	// Should send ProgressTickMsg and PhaseStartMsg
	messages := mockRunner.Messages()
	require.Len(t, messages, 2)

	// First message should be ProgressTickMsg
	progressMsg, ok := messages[0].(syncComponent.ProgressTickMsg)
	require.True(t, ok, "expected ProgressTickMsg, got %T", messages[0])
	assert.Equal(t, 0, progressMsg.Completed)
	assert.Equal(t, 3, progressMsg.Total)

	// Second message should be PhaseStartMsg
	phaseMsg, ok := messages[1].(syncComponent.PhaseStartMsg)
	require.True(t, ok, "expected PhaseStartMsg, got %T", messages[1])
	assert.Equal(t, syncComponent.PhaseRestack, phaseMsg.Phase)
}

func TestInteractiveSyncHandler_OnRestackBranch(t *testing.T) {
	mockRunner := tui.NewMockRunner()
	model := syncComponent.NewModel(0)
	handler := NewInteractiveSyncHandler(mockRunner, model, output.NewNullOutput(), output.NewNullLogger())

	// Set up initial state
	handler.OnRestackStart(2)
	mockRunner.Reset() // Clear setup messages

	// Simulate restacking a branch
	prNumber := 42
	handler.OnRestackBranch(
		"feature-branch",
		syncAction.RestackDone,
		"def5678",
		&prNumber,
		engine.LockReason(""), // lockReason
		false,                 // frozen
		true,                  // isCurrent
		"main",
		false, // reparented
		"", "",
		0,
	)

	// Should send PhaseDetailMsg and ProgressTickMsg
	messages := mockRunner.Messages()
	require.Len(t, messages, 2)

	// First message should be PhaseDetailMsg
	detailMsg, ok := messages[0].(syncComponent.PhaseDetailMsg)
	require.True(t, ok, "expected PhaseDetailMsg, got %T", messages[0])
	assert.Equal(t, syncComponent.PhaseRestack, detailMsg.Phase)
	assert.Contains(t, detailMsg.Message, "feature-branch")
	assert.Contains(t, detailMsg.Message, "PR #42")

	// Second message should be ProgressTickMsg
	progressMsg, ok := messages[1].(syncComponent.ProgressTickMsg)
	require.True(t, ok, "expected ProgressTickMsg, got %T", messages[1])
	assert.Equal(t, 1, progressMsg.Completed)
}

func TestInteractiveSyncHandler_OnRestackComplete(t *testing.T) {
	mockRunner := tui.NewMockRunner()
	model := syncComponent.NewModel(0)
	handler := NewInteractiveSyncHandler(mockRunner, model, output.NewNullOutput(), output.NewNullLogger())

	handler.OnRestackComplete(5, 2, nil, nil)

	// Verify that OnRestackComplete sends a CompleteMsg
	messages := mockRunner.Messages()
	require.Len(t, messages, 1)

	msg, ok := messages[0].(syncComponent.CompleteMsg)
	require.True(t, ok, "expected CompleteMsg, got %T", messages[0])
	assert.Contains(t, msg.Summary, "restacked 5")
	assert.Contains(t, msg.Summary, "skipped 2")
}

func TestInteractiveSyncHandler_IsInteractive(t *testing.T) {
	mockRunner := tui.NewMockRunner()
	model := syncComponent.NewModel(0)
	handler := NewInteractiveSyncHandler(mockRunner, model, output.NewNullOutput(), output.NewNullLogger())

	assert.True(t, handler.IsInteractive())
}

func TestSimpleSyncHandler_IsNotInteractive(t *testing.T) {
	handler := NewSimpleSyncHandler(output.NewNullOutput())
	assert.False(t, handler.IsInteractive())
}
