package github

import (
	"context"
	"fmt"
	"os"
	"strings"
)

// DefaultHost is the hostname of the public GitHub instance. Any other host is
// a GitHub Enterprise Server instance, which keeps its own credentials.
const DefaultHost = "github.com"

// Token resolves a GitHub credential for the host of the default remote.
//
// Resolving the host matters: `gh` stores a separate credential per hostname,
// and `gh auth token` without `--hostname` answers for whichever host `gh`
// considers default (github.com unless GH_HOST says otherwise). On an
// Enterprise remote that silently hands back a github.com token, which then
// fails against the Enterprise API.
func Token(ctx context.Context, runner GitCommandRunner) (string, error) {
	return tokenForRemote(ctx, runner, os.Getenv)
}

// tokenForRemote is Token with the environment injected. A remote that cannot
// be identified is not an error on its own: the lookup proceeds without a host
// so a repository that is not a recognizable forge remote behaves as before.
func tokenForRemote(ctx context.Context, runner GitCommandRunner, getenv func(string) string) (string, error) {
	host := ""
	if repo, err := getRemoteRepository(ctx, runner); err == nil {
		host = repo.Host
	}
	return resolveToken(ctx, runner, host, getenv)
}

// tokenForHost resolves a GitHub credential for a specific host. An empty host
// means the remote could not be identified; the lookup then falls back to
// gh's own default host selection.
func tokenForHost(ctx context.Context, runner GitCommandRunner, host string) (string, error) {
	return resolveToken(ctx, runner, host, os.Getenv)
}

// resolveToken is tokenForHost with the environment injected so tests can run
// in parallel instead of mutating the process environment.
func resolveToken(ctx context.Context, runner GitCommandRunner, host string, getenv func(string) string) (string, error) {
	if token := tokenFromEnv(host, getenv); token != "" {
		return token, nil
	}

	args := []string{"auth", "token"}
	if host != "" {
		args = append(args, "--hostname", host)
	}

	output, err := runner.RunGHCommandWithContext(ctx, args...)
	if err != nil {
		return "", fmt.Errorf("failed to get GitHub token: %w", err)
	}

	token := strings.TrimSpace(output)
	if token == "" {
		return "", fmt.Errorf("empty GitHub token")
	}

	return token, nil
}

// tokenFromEnv reads a credential for host from the environment, mirroring the
// GitHub CLI's own variable names. For an Enterprise host the Enterprise
// variables win; GITHUB_TOKEN remains the fallback for either host so existing
// single-host setups keep working.
func tokenFromEnv(host string, getenv func(string) string) string {
	names := []string{"GITHUB_TOKEN"}
	if host != "" && host != DefaultHost {
		names = append([]string{"GH_ENTERPRISE_TOKEN", "GITHUB_ENTERPRISE_TOKEN"}, names...)
	}

	for _, name := range names {
		// Trim whitespace to handle cases where secrets might have leading/trailing spaces
		if token := strings.TrimSpace(getenv(name)); token != "" {
			return token
		}
	}
	return ""
}
