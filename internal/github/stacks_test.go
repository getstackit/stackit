package github

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/go-github/v90/github"
	"github.com/stretchr/testify/require"
)

func TestCreateStack(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/repos/acme/widget/stacks", r.URL.Path)

		var body createStackRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		require.Equal(t, []int{12, 34}, body.PullRequests)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id": 99, "number": 7, "url": "https://api.github.com/repos/acme/widget/stacks/7", "base": {"ref": "main"}, "pull_requests": [{"number": 12}, {"number": 34}]}`))
	}))
	defer server.Close()

	baseURL := server.URL + "/"
	client, err := github.NewClient(github.WithHTTPClient(server.Client()), github.WithURLs(&baseURL, &baseURL))
	require.NoError(t, err)
	stackClient := &StackitGitHubClient{client: client, repo: Repo{Owner: "acme", Name: "widget"}}

	stack, err := stackClient.CreateStack(context.Background(), []int{12, 34})
	require.NoError(t, err)
	require.Equal(t, 7, stack.Number)
	require.Equal(t, "main", stack.Base.Ref)
	require.Len(t, stack.PullRequests, 2)
}

func TestCreateStackRequiresTwoPullRequests(t *testing.T) {
	t.Parallel()

	stackClient := &StackitGitHubClient{}
	_, err := stackClient.CreateStack(context.Background(), []int{12})
	require.EqualError(t, err, "a GitHub Stack requires at least two pull requests")
}
