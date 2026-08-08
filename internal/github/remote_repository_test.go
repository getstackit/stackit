package github

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/git"
)

type remoteRepositoryRunner struct {
	remoteURL string
	err       error
}

func (r remoteRepositoryRunner) GetConfig(string) (string, error) {
	return r.remoteURL, r.err
}

func (remoteRepositoryRunner) RunGHCommandWithContext(context.Context, ...string) (string, error) {
	return "", nil
}

func TestGetRemoteRepository(t *testing.T) {
	t.Run("returns the canonical hosted repository", func(t *testing.T) {
		got, err := getRemoteRepository(t.Context(), remoteRepositoryRunner{remoteURL: "git@github.com:owner/repo.git"})
		require.NoError(t, err)
		require.Equal(t, git.RemoteRepository{Host: "github.com", Owner: "owner", Name: "repo"}, got)
	})

	t.Run("preserves the HTTPS parse error context", func(t *testing.T) {
		_, err := getRemoteRepository(t.Context(), remoteRepositoryRunner{remoteURL: "https://github.com"})
		require.ErrorContains(t, err, "invalid HTTPS remote URL")
	})

	t.Run("wraps remote configuration errors", func(t *testing.T) {
		_, err := getRemoteRepository(t.Context(), remoteRepositoryRunner{err: errors.New("missing remote")})
		require.ErrorContains(t, err, "failed to get remote URL")
	})
}
