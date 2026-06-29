package github

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/go-github/v88/github"
	"github.com/stretchr/testify/require"
)

// clientForStub builds a go-github client pointed at srv so repoAccessFromClient
// hits the stub instead of api.github.com.
func clientForStub(t *testing.T, srv *httptest.Server) *github.Client {
	t.Helper()
	baseURL := srv.URL + "/"
	c, err := github.NewClient(
		github.WithHTTPClient(srv.Client()),
		github.WithURLs(&baseURL, &baseURL),
	)
	require.NoError(t, err)
	return c
}

func TestRepoAccessFromClient(t *testing.T) {
	t.Parallel()

	t.Run("readable repo returns coordinates and push permission", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			require.Equal(t, "/repos/octo/widget", r.URL.Path)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"name": "widget",
				"default_branch": "main",
				"clone_url": "https://github.com/octo/widget.git",
				"private": true,
				"owner": {"login": "octo"},
				"permissions": {"pull": true, "push": true, "admin": false}
			}`))
		}))
		t.Cleanup(srv.Close)

		access, err := repoAccessFromClient(context.Background(), clientForStub(t, srv), "octo", "widget")
		require.NoError(t, err)
		require.Equal(t, "octo", access.Owner)
		require.Equal(t, "widget", access.Name)
		require.Equal(t, "main", access.DefaultBranch)
		require.Equal(t, "https://github.com/octo/widget.git", access.CloneURL)
		require.True(t, access.Private)
		require.True(t, access.CanPush)
	})

	t.Run("read-only repo reports no push", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"name":"widget","owner":{"login":"octo"},"permissions":{"pull":true,"push":false}}`))
		}))
		t.Cleanup(srv.Close)

		access, err := repoAccessFromClient(context.Background(), clientForStub(t, srv), "octo", "widget")
		require.NoError(t, err)
		require.False(t, access.CanPush)
	})

	t.Run("404 collapses to access denied", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
		}))
		t.Cleanup(srv.Close)

		_, err := repoAccessFromClient(context.Background(), clientForStub(t, srv), "octo", "ghost")
		require.ErrorIs(t, err, ErrRepoAccessDenied)
	})

	t.Run("403 collapses to access denied", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, `{"message":"Forbidden"}`, http.StatusForbidden)
		}))
		t.Cleanup(srv.Close)

		_, err := repoAccessFromClient(context.Background(), clientForStub(t, srv), "octo", "secret")
		require.ErrorIs(t, err, ErrRepoAccessDenied)
	})
}
