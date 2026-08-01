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

func TestCreateStackRejectsMoreThanOneHundredPullRequests(t *testing.T) {
	t.Parallel()

	stackClient := &StackitGitHubClient{}
	_, err := stackClient.CreateStack(context.Background(), make([]int, MaxStackPullRequests+1))
	require.EqualError(t, err, "a GitHub Stack supports at most 100 pull requests (got 101)")
}

func TestEnsureStackExtendsExistingStack(t *testing.T) {
	t.Parallel()

	var addRequests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/widget/stacks":
			require.Equal(t, "12", r.URL.Query().Get("pull_request"))
			_, _ = w.Write([]byte(`[{"number": 7, "pull_requests": [{"number": 12}, {"number": 34}]}]`))
		case r.Method == http.MethodPost && r.URL.Path == "/repos/acme/widget/stacks/7/add":
			addRequests++
			var body createStackRequest
			require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			require.Equal(t, []int{56}, body.PullRequests)
			_, _ = w.Write([]byte(`{"number": 7, "pull_requests": [{"number": 12}, {"number": 34}, {"number": 56}]}`))
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL)
		}
	}))
	defer server.Close()

	baseURL := server.URL + "/"
	client, err := github.NewClient(github.WithHTTPClient(server.Client()), github.WithURLs(&baseURL, &baseURL))
	require.NoError(t, err)
	stackClient := &StackitGitHubClient{client: client, repo: Repo{Owner: "acme", Name: "widget"}}

	stack, action, err := EnsureStack(context.Background(), stackClient, []int{12, 34, 56})
	require.NoError(t, err)
	require.Equal(t, StackSyncExtended, action)
	require.Equal(t, 7, stack.Number)
	require.Equal(t, 1, addRequests)
}

func TestEnsureStackSkipsMatchingExistingStack(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, "/repos/acme/widget/stacks", r.URL.Path)
		_, _ = w.Write([]byte(`[{"number": 7, "pull_requests": [{"number": 12}, {"number": 34}]}]`))
	}))
	defer server.Close()

	baseURL := server.URL + "/"
	client, err := github.NewClient(github.WithHTTPClient(server.Client()), github.WithURLs(&baseURL, &baseURL))
	require.NoError(t, err)
	stackClient := &StackitGitHubClient{client: client, repo: Repo{Owner: "acme", Name: "widget"}}

	stack, action, err := EnsureStack(context.Background(), stackClient, []int{12, 34})
	require.NoError(t, err)
	require.Equal(t, StackSyncUnchanged, action)
	require.Equal(t, 7, stack.Number)
}
