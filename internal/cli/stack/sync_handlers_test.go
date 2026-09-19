package stack

import (
	"testing"

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
	model := syncComponent.NewModel(0)
	handler := NewInteractiveSyncHandler(mockRunner, model, output.NewNullOutput(), output.NewNullLogger())

	handler.Start(10)

	// Verify that Start sends a ProgressTickMsg
	messages := mockRunner.Messages()
	require.Len(t, messages, 1)

	msg, ok := messages[0].(syncComponent.ProgressTickMsg)
	require.True(t, ok, "expected ProgressTickMsg, got %T", messages[0])
	assert.Equal(t, 0, msg.Completed)
	assert.Equal(t, 0, msg.Total)
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

	// Sync has no reliable global total; completed events only print detail.
	messages := mockRunner.Messages()
	require.Len(t, messages, 1)
	detailMsg, ok := messages[0].(syncComponent.PhaseDetailMsg)
	require.True(t, ok)
	assert.Contains(t, detailMsg.Message, "main")
	assert.Contains(t, detailMsg.Message, "abc1234")
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
	handler.OnRestackBranch(handlers.RestackBranchEvent{
		Branch:      "feature-branch",
		Result:      syncAction.RestackDone,
		NewRevision: "def5678",
		PRNumber:    &prNumber,
		LockReason:  engine.LockReasonNone,
		IsCurrent:   true,
		Parent:      "main",
	})

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

	handler.OnRestackComplete(handlers.RestackSummary{Restacked: 5, Skipped: 2})

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

func TestInteractiveSyncPreservesHeldBranches(t *testing.T) {
	for _, phase := range []syncAction.Phase{syncAction.PhaseTrunk, syncAction.PhaseRestack} {
		t.Run(string(phase), func(t *testing.T) {
			runner := tui.NewMockRunner()
			h := NewInteractiveSyncHandler(runner, syncComponent.NewModel(0), output.NewNullOutput(), output.NewNullLogger())
			h.EmitEvent(syncAction.Event{Phase: phase, Type: syncAction.EventCompleted, Branch: "feat/api", HeldBy: "worktree /tmp/api has uncommitted changes"})
			detail := runner.Messages()[0].(syncComponent.PhaseDetailMsg)
			assert.Equal(t, syncComponent.MarkWarn, detail.Mark)
			assert.Contains(t, detail.Message, "/tmp/api")
			summary := h.formatSummary(syncAction.Summary{UpToDate: true})
			assert.Contains(t, summary, "Sync incomplete")
			assert.Contains(t, summary, "held 1")
			assert.NotContains(t, summary, "Everything is up to date")
		})
	}
}

func TestRestackHeldOutcome(t *testing.T) {
	runner := tui.NewMockRunner()
	out := output.NewTestOutput()
	interactive := NewInteractiveSyncHandler(runner, syncComponent.NewModel(0), output.NewNullOutput(), output.NewNullLogger())
	simple := NewSimpleSyncHandler(out)
	event := handlers.RestackBranchEvent{Branch: "feat/api", Result: handlers.RestackUnneeded, HeldBy: "worktree /tmp/api is dirty"}
	for _, h := range []handlers.RestackHandler{interactive, simple} {
		h.OnRestackStart(1)
		h.OnRestackBranch(event)
		h.OnRestackComplete(handlers.RestackSummary{})
	}
	messages := runner.Messages()
	summary := messages[len(messages)-1].(syncComponent.CompleteMsg).Summary
	assert.Contains(t, summary, "Restack incomplete: held 1")
	assert.Contains(t, out.String(), summary)
	assert.NotContains(t, out.String(), "Everything is up to date")
}

func TestConflictRecoveryTargetsReportedBranch(t *testing.T) {
	summary := formatRestackOutcome(handlers.RestackSummary{Skipped: 1, Conflicts: []string{"feat/web"}, Blocked: []string{"feat/api"}}, 0, 0)
	assert.Contains(t, summary, "⚠ Restack incomplete")
	assert.Contains(t, summary, "st restack --branch feat/web")
	assert.Contains(t, summary, "blocked 1")

	cmd := NewRestackCmd()
	require.NoError(t, cmd.ParseFlags([]string{"--branch", "feat/web"}))
	branch, err := cmd.Flags().GetString("branch")
	require.NoError(t, err)
	assert.Equal(t, "feat/web", branch)
}

func TestSyncCountsOnlyRestackResults(t *testing.T) {
	runner := tui.NewMockRunner()
	h := NewInteractiveSyncHandler(runner, syncComponent.NewModel(0), output.NewNullOutput(), output.NewNullLogger())
	h.Start(99) // The action's old estimate is not an exact total.
	h.EmitEvent(syncAction.Event{Phase: syncAction.PhaseRestack, Type: syncAction.EventStarted, Total: 2})
	h.OnRestackActivity(engine.RebaseProgress{Branch: "feat/api", Parent: "main"})
	h.OnRestackActivity(engine.RebaseProgress{Branch: "feat/api", Finished: true})
	h.EmitEvent(syncAction.Event{Phase: syncAction.PhaseGitHub, Type: syncAction.EventProgress, Branch: "feat/api"})
	require.Zero(t, h.completedOps)
	h.EmitEvent(syncAction.Event{Phase: syncAction.PhaseRestack, Type: syncAction.EventCompleted, Branch: "feat/api", NewRevision: "1234567"})
	h.EmitEvent(syncAction.Event{Phase: syncAction.PhaseRestack, Type: syncAction.EventCompleted, Branch: "feat/web"})
	require.Equal(t, 2, h.completedOps)
	require.Equal(t, 2, h.totalOps)
}
