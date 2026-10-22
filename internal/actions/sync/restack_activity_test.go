package sync

import (
	gosync "sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

// activityHandler records restack-phase events and live validation activity.
type activityHandler struct {
	NullHandler
	mu       gosync.Mutex
	activity []engine.RebaseProgress
	started  []Event
}

func (h *activityHandler) EmitEvent(event Event) {
	if event.Phase == PhaseRestack && event.Type == EventStarted && event.Total > 0 {
		h.mu.Lock()
		defer h.mu.Unlock()
		h.started = append(h.started, event)
	}
}

func (h *activityHandler) OnRestackActivity(event engine.RebaseProgress) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.activity = append(h.activity, event)
}

func TestSyncRestackReportsActivity(t *testing.T) {
	t.Parallel()
	s := scenario.NewRemoteScenario(t).
		WithStack(map[string]string{
			"P": "main",
			"C": "P",
		})
	s.Checkout("P").CommitChange("p-moved", "P updated")
	require.NoError(t, s.Engine.Rebuild("main"))

	handler := &activityHandler{}
	require.NoError(t, Action(s.Context, Options{All: true, Restack: true}, handler))
	s.ExpectBranchFixed("C")

	require.Len(t, handler.started, 1)
	require.Equal(t, 2, handler.started[0].Total)
	require.Equal(t, []engine.RebaseProgress{
		{Branch: "C", Parent: "P"},
		{Branch: "C", Parent: "P", Finished: true},
	}, handler.activity)
}
