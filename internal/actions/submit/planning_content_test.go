package submit

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"

	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/internal/github"
	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

type contentPlanEngine struct {
	engine.Engine
	statuses map[string]engine.PRSubmissionStatus
}

func (e *contentPlanEngine) BatchGetPRSubmissionStatus(context.Context, engine.Branches, engine.BranchRemoteStatuses) (map[string]engine.PRSubmissionStatus, error) {
	return e.statuses, nil
}

type contentPlanRunner struct {
	github.GitCommandRunner
	response string
	err      error
	queries  []string
}

func (*contentPlanRunner) RunGHCommandWithContext(context.Context, ...string) (string, error) {
	return "test-token", nil
}
func (r *contentPlanRunner) GetConfig(key string) (string, error) {
	if key == "remote.origin.url" {
		return "https://github.com/test/repo.git", nil
	}
	return r.GitCommandRunner.GetConfig(key)
}
func (r *contentPlanRunner) RoundTrip(req *http.Request) (*http.Response, error) {
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	r.queries = append(r.queries, string(body))
	if r.err != nil {
		return nil, r.err
	}
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(r.response))}, nil
}

type contentPlanClient struct {
	github.Client
	reads []git.PRNumber
}

func (c *contentPlanClient) Repo() github.Repo {
	return github.Repo{Owner: "test", Name: "repo"}
}
func (c *contentPlanClient) GetPullRequest(_ context.Context, number git.PRNumber) (*github.PullRequestInfo, error) {
	c.reads = append(c.reads, number)
	return &github.PullRequestInfo{Title: "remote title", Body: "remote body"}, nil
}

type contentPlanHandler struct{ events []BranchPlanEvent }

func (h *contentPlanHandler) OnEvent(event Event) {
	if plan, ok := event.(BranchPlanEvent); ok {
		h.events = append(h.events, plan)
	}
}
func (*contentPlanHandler) Confirm(string, bool) (bool, error) { return true, nil }
func (*contentPlanHandler) IsInteractive() bool                { return false }

func TestPlanningBatchesMissingPRContent(t *testing.T) {
	t.Parallel()

	const fullResponse = `{"data":{"repository":{"pr_1":{"title":"remote title","body":"remote body"},"pr_2":{"title":"remote title","body":""}}}}`
	tests := []struct {
		name       string
		response   string
		err        error
		allSkipped bool
		wantReads  []git.PRNumber
		wantBodies []string
	}{
		{
			name:       "batch hit keeps an empty remote body without refetching",
			response:   fullResponse,
			wantBodies: []string{"remote body", ""},
		},
		{
			name:       "batch miss falls back to a single fetch",
			response:   `{"data":{"repository":{"pr_1":{"title":"remote title","body":"remote body"}}}}`,
			wantReads:  []git.PRNumber{2},
			wantBodies: []string{"remote body", "remote body"},
		},
		{
			name:       "failed batch falls back for every branch",
			err:        fmt.Errorf("batch unavailable"),
			wantReads:  []git.PRNumber{1, 2},
			wantBodies: []string{"remote body", "remote body"},
		},
		{
			name:       "skipped branches stay offline",
			response:   fullResponse,
			allSkipped: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).WithLinearStack3()
			branches := engine.BranchesFromNames(s.Engine, []string{"a", "b", "c"})
			statuses := make(map[string]engine.PRSubmissionStatus)
			for i, b := range branches {
				number := git.PRNumber(i + 1)
				info := engine.NewPrInfo(engine.PrInfoFields{Number: &number, Title: "local title", State: git.PRStateOpen, Base: "main"})
				require.NoError(t, s.Engine.UpsertPrInfo(s.Context, b, info))
				statuses[b.GetName()] = engine.PRSubmissionStatus{
					Action: engine.SubmitActionUpdate, PRNumber: &number, PRInfo: info,
					NeedsUpdate: !tt.allSkipped && i < 2, Reason: "unchanged",
				}
			}
			runner := &contentPlanRunner{GitCommandRunner: s.Context.GHRunner, response: tt.response, err: tt.err}
			client := &contentPlanClient{}
			s.Context.Engine = &contentPlanEngine{Engine: s.Engine, statuses: statuses}
			s.Context.GitHubClient = client
			s.Context.GHRunner = runner
			// BatchGetPRContentGraphQL bypasses github.Client and builds its own
			// oauth2 HTTP client, so route that client's transport to the runner.
			s.Context.Context = context.WithValue(s.Context.Context, oauth2.HTTPClient, &http.Client{Transport: runner})
			handler := &contentPlanHandler{}

			infos, err := prepareBranchesForSubmit(s.Context, branches, Options{NoEdit: true}, "b", engine.BranchRemoteStatuses{}, nil, handler)
			require.NoError(t, err)
			require.Len(t, handler.events, 3)
			for i, name := range []string{"a", "b", "c"} {
				require.Equal(t, name, handler.events[i].BranchName)
			}
			if tt.allSkipped {
				require.Empty(t, infos)
				require.Empty(t, runner.queries)
				require.Empty(t, client.reads)
				return
			}

			require.Len(t, runner.queries, 1)
			require.Contains(t, runner.queries[0], "pullRequest(number: 1)")
			require.Contains(t, runner.queries[0], "pullRequest(number: 2)")
			require.NotContains(t, runner.queries[0], "pullRequest(number: 3)")
			require.Equal(t, tt.wantReads, client.reads)
			require.Len(t, infos, len(tt.wantBodies))
			for i, info := range infos {
				require.Equal(t, "local title", info.Metadata.Title, "local edits must win")
				require.Equal(t, tt.wantBodies[i], info.Metadata.Body, info.BranchName)
			}
		})
	}
}
