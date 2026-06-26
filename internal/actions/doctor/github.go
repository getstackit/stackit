package doctor

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/getstackit/stackit/internal/git"
)

// remoteCheckTimeout bounds doctor's GitHub connectivity probes. Doctor is a
// diagnostic, so a fast "unreachable" beats blocking for minutes on the git
// runner's default timeout when the network or `gh` is wedged.
const remoteCheckTimeout = 10 * time.Second

// getGitHubToken gets the GitHub token (similar to internal/github/pr_info.go).
// The context bounds the `gh auth token` probe.
func getGitHubToken(ctx context.Context, runner git.Runner) (string, error) {
	// Try environment variable first
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		// Trim whitespace to handle cases where secrets might have leading/trailing spaces
		token = strings.TrimSpace(token)
		if token != "" {
			return token, nil
		}
	}

	// Try gh CLI
	output, err := runner.RunGHCommandWithContext(ctx, "auth", "token")
	if err != nil {
		return "", fmt.Errorf("failed to get GitHub token: %w", err)
	}

	token := strings.TrimSpace(output)
	if token == "" {
		return "", fmt.Errorf("empty GitHub token")
	}

	return token, nil
}
