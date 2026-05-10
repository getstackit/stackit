package doctor

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/getstackit/stackit/internal/git"
)

// getGitHubToken gets the GitHub token (similar to internal/github/pr_info.go)
func getGitHubToken(runner git.Runner) (string, error) {
	// Try environment variable first
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		// Trim whitespace to handle cases where secrets might have leading/trailing spaces
		token = strings.TrimSpace(token)
		if token != "" {
			return token, nil
		}
	}

	// Try gh CLI
	output, err := runner.RunGHCommandWithContext(context.Background(), "auth", "token")
	if err != nil {
		return "", fmt.Errorf("failed to get GitHub token: %w", err)
	}

	token := strings.TrimSpace(output)
	if token == "" {
		return "", fmt.Errorf("empty GitHub token")
	}

	return token, nil
}
