package actions

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

// activityRestackHandler records live validation activity alongside results.
type activityRestackHandler struct {
	NullRestackHandler
	mu     sync.Mutex
	events []engine.RebaseProgress
}

func (h *activityRestackHandler) OnRestackActivity(event engine.RebaseProgress) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.events = append(h.events, event)
}

func (h *activityRestackHandler) recorded() []engine.RebaseProgress {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]engine.RebaseProgress(nil), h.events...)
}

func TestRestackActionReportsActivity(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		parallel bool
	}{
		{name: "sequential"},
		{name: "parallel", parallel: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
				WithStack(map[string]string{
					"alpha": "main",
					"beta":  "main",
				})
			s.Checkout("main").CommitChange("trunk-moved", "advance trunk")
			require.NoError(t, s.Engine.Rebuild(t.Context(), "main"))

			plan, err := PlanRestack(s.Context, restackDepsFrom(s.Context), RestackOptions{AllStacks: true, Parallel: tc.parallel, Jobs: 2})
			require.NoError(t, err)
			handler := &activityRestackHandler{}
			require.NoError(t, RestackAction(s.Context, restackDepsFrom(s.Context), plan, handler))

			started := map[string]string{}
			finished := map[string]bool{}
			for _, event := range handler.recorded() {
				if event.Finished {
					finished[event.Branch] = true
					continue
				}
				started[event.Branch] = event.Parent
			}
			// Parent is the planned parent's name, not the spec's revision.
			require.Equal(t, map[string]string{"alpha": "main", "beta": "main"}, started)
			require.Equal(t, map[string]bool{"alpha": true, "beta": true}, finished)
		})
	}
}
