package stack

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	syncAction "github.com/getstackit/stackit/internal/actions/sync"
	"github.com/getstackit/stackit/internal/cli/common"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/internal/handlers"
	"github.com/getstackit/stackit/internal/output"
	"github.com/getstackit/stackit/internal/tui"
	syncComponent "github.com/getstackit/stackit/internal/tui/components/sync"
)

func TestInteractiveSyncHandler_Start(t *testing.T) {
	t.Parallel()
	mockRunner := tui.NewMockRunner()
	handler := NewInteractiveSyncHandler(mockRunner, syncComponent.NewModel(), output.NewNullOutput(), output.NewNullLogger())

	handler.Start()

	// Sync has no reliable up-front total, so Start draws nothing.
	require.Empty(t, mockRunner.Messages())
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
	t.Parallel()
	mockRunner := tui.NewMockRunner()
	model := syncComponent.NewModel()
	handler := NewInteractiveSyncHandler(mockRunner, model, output.NewNullOutput(), output.NewNullLogger())

	handler.Start()

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
	require.True(t, ok, "expected PhaseDetailMsg, got %T", messages[0])
	require.Equal(t, syncComponent.PhaseTrunk, detailMsg.Phase)
	require.Equal(t, syncComponent.MarkDone, detailMsg.Mark)
	require.Contains(t, detailMsg.Message, "main")
	require.Contains(t, detailMsg.Message, "abc1234")
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
	model := syncComponent.NewModel()
	handler := NewInteractiveSyncHandler(mockRunner, model, output.NewNullOutput(), output.NewNullLogger())

	// Set up initial state
	handler.OnRestackStart(2)
	mockRunner.Reset() // Clear setup messages

	// Simulate restacking a branch
	prNumber := git.PRNumber(42)
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
	model := syncComponent.NewModel()
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
	model := syncComponent.NewModel()
	handler := NewInteractiveSyncHandler(mockRunner, model, output.NewNullOutput(), output.NewNullLogger())

	assert.True(t, handler.IsInteractive())
}

func TestSimpleSyncHandler_IsNotInteractive(t *testing.T) {
	handler := NewSimpleSyncHandler(output.NewNullOutput())
	assert.False(t, handler.IsInteractive())
}

func TestInteractiveSyncPreservesHeldBranches(t *testing.T) {
	t.Parallel()
	for _, phase := range []syncAction.Phase{syncAction.PhaseTrunk, syncAction.PhaseRestack} {
		t.Run(string(phase), func(t *testing.T) {
			t.Parallel()
			runner := tui.NewMockRunner()
			h := NewInteractiveSyncHandler(runner, syncComponent.NewModel(), output.NewNullOutput(), output.NewNullLogger())
			h.EmitEvent(syncAction.Event{Phase: phase, Type: syncAction.EventCompleted, Branch: "feat/api", HeldBy: "worktree /tmp/api has uncommitted changes"})
			detail := runner.Messages()[0].(syncComponent.PhaseDetailMsg)
			assert.Equal(t, syncComponent.MarkWarn, detail.Mark)
			assert.Contains(t, detail.Message, "/tmp/api")
			summary := h.formatSummary(syncAction.Summary{HeldBranches: []string{"feat/api"}})
			assert.Contains(t, summary, "Sync incomplete")
			assert.Contains(t, summary, "held 1")
			assert.NotContains(t, summary, "Everything is up to date")
		})
	}
}

func TestRestackHeldOutcome(t *testing.T) {
	t.Parallel()
	held := []handlers.RestackHeldInfo{{Branch: "feat/api", Reason: "worktree /tmp/api has uncommitted changes"}}
	tests := []struct {
		name   string
		events []handlers.RestackBranchEvent
	}{
		{
			// The engine held the branch mid-run and reported a per-branch event.
			name:   "held during restack",
			events: []handlers.RestackBranchEvent{{Branch: "feat/api", Result: handlers.RestackUnneeded, HeldBy: held[0].Reason}},
		},
		{
			// Planning pruned the branch, so only the summary knows about it
			// while its already-current child still reports a plain row.
			name:   "held while planning",
			events: []handlers.RestackBranchEvent{{Branch: "feat/ui", Result: handlers.RestackUnneeded}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			runner := tui.NewMockRunner()
			out := output.NewTestOutput()
			interactive := NewInteractiveSyncHandler(runner, syncComponent.NewModel(), output.NewNullOutput(), output.NewNullLogger())
			simple := NewSimpleSyncHandler(out)
			for _, h := range []handlers.RestackHandler{interactive, simple} {
				h.OnRestackStart(len(tt.events))
				for _, event := range tt.events {
					h.OnRestackBranch(event)
				}
				h.OnRestackComplete(handlers.RestackSummary{Held: held})
			}
			messages := runner.Messages()
			summary := messages[len(messages)-1].(syncComponent.CompleteMsg).Summary
			assert.Contains(t, summary, "⚠ Restack incomplete: held 1 (worktree)")
			assert.Contains(t, out.String(), summary)
			assert.NotContains(t, out.String(), "Everything is up to date")
			assert.NotContains(t, out.String(), "Summary:")
		})
	}
}

func TestSimpleSyncCompleteReportsHolds(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		summary syncAction.Summary
		want    string
	}{
		{
			name:    "held restack branches",
			summary: syncAction.Summary{HeldBranches: []string{"y", "z"}},
			want:    "⚠ Sync incomplete: held 2 (worktree)",
		},
		{
			name:    "held trunk alongside cleanup",
			summary: syncAction.Summary{HeldBranches: []string{"main"}, BranchesDeleted: 1},
			want:    "⚠ Sync incomplete: held 1 (worktree), deleted 1",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			out := output.NewTestOutput()
			NewSimpleSyncHandler(out).Complete(tt.summary)
			assert.Contains(t, out.String(), tt.want)
			assert.NotContains(t, out.String(), "Everything is up to date")
		})
	}
}

func TestConflictRecoveryTargetsReportedBranch(t *testing.T) {
	t.Parallel()
	summary := common.FormatRestackOutcome(handlers.RestackSummary{Skipped: 1, Conflicts: []string{"feat/web"}, Blocked: []string{"feat/api"}}, 0)
	assert.Contains(t, summary, "⚠ Restack incomplete")
	assert.Contains(t, summary, "st restack --branch feat/web")
	assert.Contains(t, summary, "blocked 1")

	cmd := NewRestackCmd()
	require.NoError(t, cmd.ParseFlags([]string{"--branch", "feat/web"}))
	branch, err := cmd.Flags().GetString("branch")
	require.NoError(t, err)
	assert.Equal(t, "feat/web", branch)
}

func TestSyncSummaryOmitsEmptySummaryLine(t *testing.T) {
	t.Parallel()
	require.Empty(t, formatSyncSummary(syncAction.Summary{}))
	require.Contains(t, formatSyncSummary(syncAction.Summary{SkippedStacks: []string{"feat/api"}}), "Sync incomplete")
}

// TestSyncHandlersAgreeOnRoutineRows drives the same sync events through the
// interactive and streaming handlers: routine "already current" rows are hidden
// by both, while anything the user must know about prints in both.
func TestSyncHandlersAgreeOnRoutineRows(t *testing.T) {
	t.Parallel()
	ev := func(phase syncAction.Phase, typ syncAction.EventType, branch string) syncAction.Event {
		return syncAction.Event{Phase: phase, Type: typ, Branch: branch}
	}
	locked := ev(syncAction.PhaseRestack, syncAction.EventCompleted, "locked-branch")
	locked.LockReason = git.LockReasonUser
	frozen := ev(syncAction.PhaseRestack, syncAction.EventCompleted, "frozen-branch")
	frozen.Frozen = true
	held := ev(syncAction.PhaseRestack, syncAction.EventCompleted, "held-branch")
	held.HeldBy = "worktree /tmp/held has uncommitted changes"
	heldTrunk := ev(syncAction.PhaseTrunk, syncAction.EventCompleted, "held-trunk")
	heldTrunk.HeldBy = "worktree /tmp/main has uncommitted changes"
	conflict := ev(syncAction.PhaseRestack, syncAction.EventSkipped, "conflict-branch")
	conflict.Conflict = true
	diverged := ev(syncAction.PhaseBranches, syncAction.EventSkipped, "diverged-branch")
	diverged.Conflict = true
	moved := ev(syncAction.PhaseTrunk, syncAction.EventCompleted, "moved-trunk")
	moved.NewRevision = "abc1234"
	metadata := syncAction.Event{Phase: syncAction.PhaseGitHub, Type: syncAction.EventCompleted, Message: "Updated PR metadata for 2 branches"}

	tests := []struct {
		name  string
		event syncAction.Event
		shown string // substring expected in both outputs; empty means hidden
	}{
		{"trunk up to date", ev(syncAction.PhaseTrunk, syncAction.EventCompleted, "main"), ""},
		{"synced branch up to date", ev(syncAction.PhaseBranches, syncAction.EventCompleted, "synced-branch"), ""},
		{"restacked branch up to date", ev(syncAction.PhaseRestack, syncAction.EventCompleted, "current-branch"), ""},
		{"trunk fast-forwarded", moved, "moved-trunk"},
		{"locked branch", locked, "locked-branch"},
		{"frozen branch", frozen, "frozen-branch"},
		{"held branch", held, "/tmp/held"},
		{"held trunk", heldTrunk, "/tmp/main"},
		{"conflict", conflict, "conflict-branch"},
		{"diverged branch", diverged, "diverged-branch"},
		{"pr metadata batch", metadata, "Updated PR metadata for 2 branches"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			runner := tui.NewMockRunner()
			interactive := NewInteractiveSyncHandler(runner, syncComponent.NewModel(), output.NewNullOutput(), output.NewNullLogger())
			out := output.NewTestOutput()
			simple := NewSimpleSyncHandler(out)
			for _, h := range []syncAction.Handler{interactive, simple} {
				h.EmitEvent(syncAction.Event{Phase: tt.event.Phase, Type: syncAction.EventStarted})
				h.EmitEvent(tt.event)
			}

			var details []string
			for _, msg := range runner.Messages() {
				if detail, ok := msg.(syncComponent.PhaseDetailMsg); ok {
					details = append(details, detail.Message)
				}
			}
			if tt.shown == "" {
				require.Empty(t, details, "interactive handler should hide routine rows")
				require.Empty(t, out.String(), "streaming handler should hide routine rows")
				return
			}
			require.Len(t, details, 1, "interactive handler should show the row")
			require.Contains(t, details[0], tt.shown)
			require.Contains(t, out.String(), tt.shown, "streaming handler should show the row")
		})
	}
}

func TestSyncPRMetadataProgressIsLiveOnly(t *testing.T) {
	t.Parallel()
	progress := syncAction.Event{Phase: syncAction.PhaseGitHub, Type: syncAction.EventProgress, Message: "Updating PR metadata for 2 branches..."}

	runner := tui.NewMockRunner()
	NewInteractiveSyncHandler(runner, syncComponent.NewModel(), output.NewNullOutput(), output.NewNullLogger()).EmitEvent(progress)
	messages := runner.Messages()
	require.Len(t, messages, 1)
	detail, ok := messages[0].(syncComponent.PhaseDetailMsg)
	require.True(t, ok, "expected PhaseDetailMsg, got %T", messages[0])
	require.Equal(t, syncComponent.MarkInProgress, detail.Mark, "the batch in flight belongs on the live line")
	require.Equal(t, progress.Message, detail.Message)

	out := output.NewTestOutput()
	NewSimpleSyncHandler(out).EmitEvent(progress)
	require.Contains(t, out.String(), progress.Message)
}

func TestSyncCountsOnlyRestackResults(t *testing.T) {
	t.Parallel()
	runner := tui.NewMockRunner()
	h := NewInteractiveSyncHandler(runner, syncComponent.NewModel(), output.NewNullOutput(), output.NewNullLogger())
	h.Start()
	h.EmitEvent(syncAction.Event{Phase: syncAction.PhaseRestack, Type: syncAction.EventStarted, Total: 2})
	h.OnRestackActivity(engine.RebaseProgress{Branch: "feat/api", Parent: "main"})
	h.OnRestackActivity(engine.RebaseProgress{Branch: "feat/api", Finished: true})
	h.EmitEvent(syncAction.Event{Phase: syncAction.PhaseGitHub, Type: syncAction.EventProgress, Message: "Updating PR metadata for 1 branch..."})
	require.Zero(t, h.completedOps)
	h.EmitEvent(syncAction.Event{Phase: syncAction.PhaseRestack, Type: syncAction.EventCompleted, Branch: "feat/api", NewRevision: "1234567"})
	// A hidden up-to-date row still counts toward k/N.
	h.EmitEvent(syncAction.Event{Phase: syncAction.PhaseRestack, Type: syncAction.EventCompleted, Branch: "feat/web"})
	require.Equal(t, 2, h.completedOps)
	require.Equal(t, 2, h.totalOps)
}

func TestRestackActivityOnlyForInteractiveHandler(t *testing.T) {
	t.Parallel()
	require.Nil(t, handlers.RestackActivity(NewSimpleSyncHandler(output.NewTestOutput())), "simple handler prints results only")
	require.Nil(t, handlers.RestackActivity(handlers.NewJSONRestackHandler()), "JSON output must not carry live activity")

	runner := tui.NewMockRunner()
	h := NewInteractiveSyncHandler(runner, syncComponent.NewModel(), output.NewNullOutput(), output.NewNullLogger())
	activity := handlers.RestackActivity(h)
	require.NotNil(t, activity)
	activity(engine.RebaseProgress{Branch: "feat/api", Parent: "main"})
	require.Contains(t, runner.Messages(), syncComponent.ActivityMsg{Branch: "feat/api", Parent: "main"})
}
