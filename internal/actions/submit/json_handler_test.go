package submit

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestJSONHandlerCollectsPlanAndResults(t *testing.T) {
	t.Parallel()

	pr := 934
	h := NewJSONHandler()

	h.OnEvent(BranchPlanEvent{BranchName: "update-me", Action: "update", PRNumber: &pr})
	h.OnEvent(BranchPlanEvent{BranchName: "create-me", Action: "create"})
	h.OnEvent(BranchPlanEvent{BranchName: "skip-me", Skipped: true, SkipReason: "no changes"})
	h.OnEvent(BranchProgressEvent{
		BranchName: "create-me",
		Status:     StatusDone,
		URL:        "https://github.com/getstackit/stackit/pull/935",
	})
	h.OnEvent(BranchProgressEvent{BranchName: "update-me", Status: StatusError, Error: errors.New("remote rejected")})
	h.OnEvent(BranchWarningEvent{BranchName: "create-me", Warning: "failed to add labels"})
	// Footer sync re-emits done without a URL; learned fields must survive.
	h.OnEvent(BranchProgressEvent{BranchName: "create-me", Status: StatusDone})
	h.OnEvent(CompletionEvent{Outcome: OutcomeComplete, Message: "Submit complete"})

	require.Equal(t, OutcomeComplete, h.Result.Outcome)
	require.Equal(t, "Submit complete", h.Result.Message)
	require.Len(t, h.Result.Branches, 3)

	updated := h.Result.Branches[0]
	require.Equal(t, "update-me", updated.Branch)
	require.Equal(t, "update", updated.Action)
	require.Equal(t, string(StatusError), updated.Status)
	require.Equal(t, 934, *updated.PR)
	require.Equal(t, "remote rejected", updated.Error)

	created := h.Result.Branches[1]
	require.Equal(t, string(StatusDone), created.Status)
	require.Equal(t, "https://github.com/getstackit/stackit/pull/935", created.URL)
	require.Equal(t, 935, *created.PR, "PR number derives from the URL when the plan had none")
	require.Equal(t, []string{"failed to add labels"}, created.Warnings)

	skipped := h.Result.Branches[2]
	require.Equal(t, string(StatusSkipped), skipped.Status)
	require.Equal(t, "no changes", skipped.SkipReason)
}

func TestJSONHandlerSetErrorDefaultsOutcome(t *testing.T) {
	t.Parallel()

	h := NewJSONHandler()
	h.SetError(errors.New("boom"))

	require.Equal(t, "boom", h.Result.Error)
	require.Equal(t, OutcomeFailed, h.Result.Outcome)
}

func TestJSONHandlerCollectsEveryGitHubStackEvent(t *testing.T) {
	t.Parallel()

	h := NewJSONHandler()
	h.OnEvent(GitHubStackSyncedEvent{Number: 1, PullRequests: []int{10, 11}, Action: "created"})
	h.OnEvent(GitHubStackSkippedEvent{Reason: "first skipped component"})

	require.NotNil(t, h.Result.GitHubStack, "the legacy field remains available for one Stack")
	require.Equal(t, "first skipped component", h.Result.GitHubStackSkipped)

	h.OnEvent(GitHubStackSyncedEvent{Number: 2, PullRequests: []int{12, 13}, Action: "extended"})
	h.OnEvent(GitHubStackSkippedEvent{Reason: "second skipped component"})

	require.Equal(t, []JSONGitHubStackResult{
		{Number: 1, PullRequests: []int{10, 11}, Action: "created"},
		{Number: 2, PullRequests: []int{12, 13}, Action: "extended"},
	}, h.Result.GitHubStacks)
	require.Equal(t, []string{"first skipped component", "second skipped component"}, h.Result.GitHubStackSkips)
	require.Nil(t, h.Result.GitHubStack, "the legacy field must not silently discard all but the final Stack")
	require.Empty(t, h.Result.GitHubStackSkipped, "the legacy field must not silently discard skipped components")
}
