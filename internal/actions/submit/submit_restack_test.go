package submit_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/actions"
	"github.com/getstackit/stackit/internal/actions/submit"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

type captureRestackHandler struct {
	noopHandler
	branches []actions.RestackBranchEvent
	summary  *actions.RestackSummary
}

func (h *captureRestackHandler) OnEvent(e submit.Event) {
	switch ev := e.(type) {
	case submit.RestackBranchEvent:
		h.branches = append(h.branches, ev.RestackBranchEvent)
	case submit.RestackEvent:
		if ev.Completed {
			h.summary = &ev.Summary
		}
	}
}

// TestSubmitReportsRestackPerBranch pins that submit's pre-submit restack goes
// through the shared pipeline: each branch is reported with the same event
// restack and sync emit, and the completion carries the summary.
func TestSubmitReportsRestackPerBranch(t *testing.T) {
	t.Parallel()

	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
		WithStack(map[string]string{"A": "main", "B": "A"})

	_, err := s.Scene.Repo.CreateBareRemote("origin")
	require.NoError(t, err)

	config := testhelpers.NewMockGitHubServerConfig()
	rawClient, owner, repo := testhelpers.NewMockGitHubClient(t, config)
	s.Context.GitHubClient = testhelpers.NewMockGitHubClientInterface(rawClient, owner, repo, config)

	// Move trunk so both branches need a restack.
	s.Checkout("main")
	s.CommitChange("trunk.txt", "chore: trunk moves on")
	s.Checkout("B")
	s.Rebuild()

	handler := &captureRestackHandler{}
	opts := submit.Options{StackRange: engine.StackRangeFull(), NoEdit: true, Restack: true}
	require.NoError(t, submit.Action(s.Context, opts, handler))

	results := map[string]actions.RestackResult{}
	for _, event := range handler.branches {
		results[event.Branch] = event.Result
	}
	require.Equal(t, map[string]actions.RestackResult{
		"A": actions.RestackDone,
		"B": actions.RestackDone,
	}, results)
	require.NotNil(t, handler.summary)
	require.Equal(t, 2, handler.summary.Restacked)
}
