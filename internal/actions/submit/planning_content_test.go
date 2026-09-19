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
	runner   git.Runner
	statuses map[string]engine.PRSubmissionStatus
}

func (e *contentPlanEngine) Git() git.Runner { return e.runner }
func (e *contentPlanEngine) BatchGetPRSubmissionStatusWithRemote(engine.Branches, engine.BranchRemoteStatuses) (map[string]engine.PRSubmissionStatus, error) {
	return e.statuses, nil
}

type contentPlanRunner struct {
	git.Runner
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
	return r.Runner.GetConfig(key)
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
	reads []int
}

func (c *contentPlanClient) Repo() github.Repo {
	return github.Repo{Owner: "test", Name: "repo"}
}
func (c *contentPlanClient) GetPullRequest(_ context.Context, number int) (*github.PullRequestInfo, error) {
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
	for _, mode := range []string{"success", "partial", "failure", "all skipped"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).WithLinearStack3()
			branches := engine.BranchesFromNames(s.Engine, []string{"a", "b", "c"})
			statuses := make(map[string]engine.PRSubmissionStatus)
			for i, b := range branches {
				number := i + 1
				info := engine.NewPrInfo(&number, "local title", "", git.PRStateOpen, "main", "", false)
				require.NoError(t, s.Engine.UpsertPrInfo(s.Context, b, info))
				statuses[b.GetName()] = engine.PRSubmissionStatus{
					Action: engine.SubmitActionUpdate, PRNumber: &number, PRInfo: info,
					NeedsUpdate: mode != "all skipped" && i < 2, Reason: "unchanged",
				}
			}
			runner := &contentPlanRunner{Runner: s.Engine.Git(), response: `{"data":{"repository":{"pr_1":{"title":"remote title","body":"remote body"},"pr_2":{"title":"remote title","body":""}}}}`}
			if mode == "partial" {
				runner.response = `{"data":{"repository":{"pr_1":{"title":"remote title","body":"remote body"}}}}`
			}
			if mode == "failure" {
				runner.err = fmt.Errorf("batch unavailable")
			}
			client := &contentPlanClient{}
			s.Context.Engine = &contentPlanEngine{Engine: s.Engine, runner: runner, statuses: statuses}
			s.Context.GitHubClient = client
			s.Context.Context = context.WithValue(s.Context.Context, oauth2.HTTPClient, &http.Client{Transport: runner})
			handler := &contentPlanHandler{}
			infos, err := prepareBranchesForSubmit(s.Context, branches, Options{NoEdit: true}, "b", engine.BranchRemoteStatuses{}, nil, handler)
			require.NoError(t, err)
			require.Len(t, handler.events, 3)
			for i, name := range []string{"a", "b", "c"} {
				require.Equal(t, name, handler.events[i].BranchName)
			}
			if mode == "all skipped" {
				require.Empty(t, infos)
				require.Empty(t, runner.queries)
				require.Empty(t, client.reads)
				return
			}
			require.Len(t, runner.queries, 1)
			require.Contains(t, runner.queries[0], "pullRequest(number: 1)")
			require.Contains(t, runner.queries[0], "pullRequest(number: 2)")
			require.NotContains(t, runner.queries[0], "pullRequest(number: 3)")
			require.Len(t, infos, 2)
			for _, info := range infos {
				require.Equal(t, "local title", info.Metadata.Title, "local edits must win")
			}
			require.Equal(t, "remote body", infos[0].Metadata.Body)
			switch mode {
			case "success":
				require.Empty(t, client.reads, "an empty remote body is a batch hit")
			case "partial":
				require.Equal(t, []int{2}, client.reads)
			case "failure":
				require.Equal(t, []int{1, 2}, client.reads)
			}
		})
	}
}
