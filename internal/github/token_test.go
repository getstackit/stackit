package github

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

// tokenRunner records the gh invocation so tests can assert on the hostname
// that was passed through.
type tokenRunner struct {
	remoteURL string
	configErr error
	token     string
	ghErr     error

	ghArgs []string
}

func (r *tokenRunner) GetConfig(string) (string, error) {
	return r.remoteURL, r.configErr
}

func (r *tokenRunner) RunGHCommandWithContext(_ context.Context, args ...string) (string, error) {
	r.ghArgs = args
	if r.ghErr != nil {
		return "", r.ghErr
	}
	return r.token, nil
}

// emptyEnv stands in for a shell with no GitHub credentials exported.
func emptyEnv(string) string { return "" }

func fakeEnv(vars map[string]string) func(string) string {
	return func(name string) string { return vars[name] }
}

func TestResolveTokenPassesHostToGH(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		host     string
		wantArgs []string
	}{
		{
			name:     "enterprise host selects the enterprise credential",
			host:     "github.example.com",
			wantArgs: []string{"auth", "token", "--hostname", "github.example.com"},
		},
		{
			name:     "github.com host is explicit too",
			host:     DefaultHost,
			wantArgs: []string{"auth", "token", "--hostname", "github.com"},
		},
		{
			name:     "unknown host falls back to gh's default host",
			host:     "",
			wantArgs: []string{"auth", "token"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			runner := &tokenRunner{token: "gho_test\n"}
			token, err := resolveToken(t.Context(), runner, tt.host, emptyEnv)
			require.NoError(t, err)
			require.Equal(t, "gho_test", token)
			require.Equal(t, tt.wantArgs, runner.ghArgs)
		})
	}
}

func TestTokenResolvesHostFromRemote(t *testing.T) {
	t.Parallel()

	t.Run("uses the enterprise remote's host", func(t *testing.T) {
		t.Parallel()
		runner := &tokenRunner{remoteURL: "git@github.example.com:owner/repo.git", token: "gho_test"}
		token, err := tokenForRemote(t.Context(), runner, emptyEnv)
		require.NoError(t, err)
		require.Equal(t, "gho_test", token)
		require.Equal(t, []string{"auth", "token", "--hostname", "github.example.com"}, runner.ghArgs)
	})

	t.Run("omits the host when the remote cannot be parsed", func(t *testing.T) {
		t.Parallel()
		runner := &tokenRunner{configErr: errors.New("no remote"), token: "gho_test"}
		token, err := tokenForRemote(t.Context(), runner, emptyEnv)
		require.NoError(t, err)
		require.Equal(t, "gho_test", token)
		require.Equal(t, []string{"auth", "token"}, runner.ghArgs)
	})
}

func TestResolveTokenErrors(t *testing.T) {
	t.Parallel()

	t.Run("wraps a gh failure", func(t *testing.T) {
		t.Parallel()
		runner := &tokenRunner{ghErr: errors.New("not logged in")}
		_, err := resolveToken(t.Context(), runner, "github.example.com", emptyEnv)
		require.ErrorContains(t, err, "failed to get GitHub token")
	})

	t.Run("rejects an empty token", func(t *testing.T) {
		t.Parallel()
		runner := &tokenRunner{token: "  \n"}
		_, err := resolveToken(t.Context(), runner, "github.example.com", emptyEnv)
		require.ErrorContains(t, err, "empty GitHub token")
	})
}

func TestTokenFromEnv(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		host string
		vars map[string]string
		want string
	}{
		{
			name: "enterprise host prefers GH_ENTERPRISE_TOKEN",
			host: "github.example.com",
			vars: map[string]string{"GH_ENTERPRISE_TOKEN": "enterprise", "GITHUB_TOKEN": "dotcom"},
			want: "enterprise",
		},
		{
			name: "enterprise host accepts GITHUB_ENTERPRISE_TOKEN",
			host: "github.example.com",
			vars: map[string]string{"GITHUB_ENTERPRISE_TOKEN": "enterprise", "GITHUB_TOKEN": "dotcom"},
			want: "enterprise",
		},
		{
			name: "enterprise host falls back to GITHUB_TOKEN",
			host: "github.example.com",
			vars: map[string]string{"GITHUB_TOKEN": " dotcom "},
			want: "dotcom",
		},
		{
			name: "github.com ignores the enterprise variables",
			host: DefaultHost,
			vars: map[string]string{"GH_ENTERPRISE_TOKEN": "enterprise", "GITHUB_TOKEN": "dotcom"},
			want: "dotcom",
		},
		{
			name: "unknown host ignores the enterprise variables",
			host: "",
			vars: map[string]string{"GH_ENTERPRISE_TOKEN": "enterprise"},
			want: "",
		},
		{
			name: "no credentials exported",
			host: "github.example.com",
			vars: nil,
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, tokenFromEnv(tt.host, fakeEnv(tt.vars)))
		})
	}
}

func TestResolveTokenPrefersEnvOverGH(t *testing.T) {
	t.Parallel()

	runner := &tokenRunner{token: "from-gh"}
	token, err := resolveToken(t.Context(), runner, DefaultHost, fakeEnv(map[string]string{"GITHUB_TOKEN": "from-env"}))
	require.NoError(t, err)
	require.Equal(t, "from-env", token)
	require.Nil(t, runner.ghArgs)
}

func TestGetCurrentUserPassesHostToGH(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		host     string
		wantArgs []string
	}{
		{
			name:     "enterprise host is passed through",
			host:     "github.example.com",
			wantArgs: []string{"api", "--hostname", "github.example.com", "user", "-q", ".login"},
		},
		{
			name:     "unknown host falls back to gh's default host",
			host:     "",
			wantArgs: []string{"api", "user", "-q", ".login"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			runner := &tokenRunner{token: "octocat\n"}
			client := &StackitGitHubClient{runner: runner, host: tt.host}
			user, err := client.GetCurrentUser(t.Context())
			require.NoError(t, err)
			require.Equal(t, "octocat", user)
			require.Equal(t, tt.wantArgs, runner.ghArgs)
		})
	}
}
