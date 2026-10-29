package submit_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/actions/submit"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

// TestSubmitRetriesAfterPartialFailure covers the partial-submit shape: the
// push lands but the pull request write fails. The successful push makes the
// branch match its remote, which is the only signal an ordinary submit has, so
// without a record of the unfinished write the next submit reports "nothing to
// submit" and the pull request keeps its stale title, body, and base.
func TestSubmitRetriesAfterPartialFailure(t *testing.T) {
	t.Parallel()

	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
		WithStack(map[string]string{"feature": "main"})

	_, err := s.Scene.Repo.CreateBareRemote("origin")
	require.NoError(t, err)

	config := testhelpers.NewMockGitHubServerConfig()
	rawClient, owner, repo := testhelpers.NewMockGitHubClient(t, config)
	s.Context.GitHubClient = testhelpers.NewMockGitHubClientInterface(rawClient, owner, repo, config)

	opts := submit.Options{NoEdit: true}

	// First submit creates the PR and pushes the branch.
	require.NoError(t, submit.Action(s.Context, opts, &noopHandler{}))
	require.NotEmpty(t, config.CreatedPRs, "first submit should create a PR")

	// New work, and a GitHub that accepts the push but rejects the PR update.
	s.CommitChange("more.txt", "feat: more work")
	s.Rebuild()
	config.FailRequests("PATCH", errors.New("boom"))

	require.Error(t, submit.Action(s.Context, opts, &noopHandler{}), "PR update failure should fail the submit")

	// The push landed, so the branch now matches its remote: every ordinary
	// signal reads as up to date.
	branch := s.Engine.GetBranch("feature")
	branches := engine.BranchesOf(branch)
	remoteStatuses := s.Engine.ReadBranchRemoteStatuses(t.Context(), branches)
	require.True(t, remoteStatuses.ForBranch(branch).Matches(),
		"the failed submit should still have pushed the branch")

	statuses, err := s.Engine.BatchGetPRSubmissionStatus(t.Context(), branches, remoteStatuses)
	require.NoError(t, err)
	require.True(t, statuses["feature"].NeedsUpdate,
		"a submit whose PR write failed must still be pending")

	// With GitHub healthy again the retry goes through and clears the marker.
	config.StopFailingRequests("PATCH")
	require.NoError(t, submit.Action(s.Context, opts, &noopHandler{}))
	require.NotEmpty(t, config.UpdatedPRs, "the retry should have updated the PR")

	remoteStatuses = s.Engine.ReadBranchRemoteStatuses(t.Context(), branches)
	statuses, err = s.Engine.BatchGetPRSubmissionStatus(t.Context(), branches, remoteStatuses)
	require.NoError(t, err)
	require.False(t, statuses["feature"].NeedsUpdate,
		"a completed submit should leave nothing pending")
}

// TestSubmitPendingIsClearedPerBranch keeps the retry narrow: when one PR write
// in a stack fails, only that branch stays pending. Re-submitting the branches
// that already landed would churn their pull requests on every later run.
func TestSubmitPendingIsClearedPerBranch(t *testing.T) {
	t.Parallel()

	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
		WithStack(map[string]string{"A": "main", "B": "A"})

	_, err := s.Scene.Repo.CreateBareRemote("origin")
	require.NoError(t, err)

	config := testhelpers.NewMockGitHubServerConfig()
	rawClient, owner, repo := testhelpers.NewMockGitHubClient(t, config)
	s.Context.GitHubClient = testhelpers.NewMockGitHubClientInterface(rawClient, owner, repo, config)

	opts := submit.Options{StackRange: engine.StackRangeFull(), NoEdit: true, Restack: true}
	require.NoError(t, submit.Action(s.Context, opts, &noopHandler{}))
	require.Len(t, config.CreatedPRs, 2)

	prNumbers := map[string]int{}
	for _, pullRequest := range config.CreatedPRs {
		prNumbers[*pullRequest.Head.Ref] = *pullRequest.Number
	}

	// New work at the bottom of the stack moves both branches, with GitHub
	// rejecting only B's PR update.
	s.Checkout("A")
	s.CommitChange("a2.txt", "feat: more A")
	s.Rebuild()
	config.FailRequests(fmt.Sprintf("PATCH /repos/%s/%s/pulls/%d", owner, repo, prNumbers["B"]), errors.New("boom"))

	require.Error(t, submit.Action(s.Context, opts, &noopHandler{}))

	pending := func(branch string) bool {
		t.Helper()
		meta, err := s.Metadata.ReadLocalMetadata(t.Context(), branch).One()
		require.NoError(t, err)
		return meta != nil && meta.SubmitPending
	}
	require.False(t, pending("A"), "A's PR write landed, so it should not be pending")
	require.True(t, pending("B"), "B's PR write failed, so it should still be pending")
}
