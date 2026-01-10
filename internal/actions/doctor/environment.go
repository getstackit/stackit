package doctor

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"stackit.dev/stackit/internal/git"
	"stackit.dev/stackit/internal/github"
)

// checkEnvironment performs environment-related checks
func checkEnvironment(runner git.Runner, handler Handler, warnings int, errors int) (int, int) {
	// Check git version
	gitVersion, err := exec.Command("git", "version").Output()
	if err != nil {
		errors++
		handler.OnCheck("git", CheckError, "git is not installed or not in PATH")
	} else {
		version := strings.TrimSpace(string(gitVersion))
		handler.OnCheck("git", CheckPassed, version)
	}

	// Check gh CLI
	ghVersion, err := exec.Command("gh", "version").Output()
	if err != nil {
		warnings++
		handler.OnCheck("gh", CheckWarning, "GitHub CLI (gh) is not installed or not in PATH")
	} else {
		version := strings.TrimSpace(string(ghVersion))
		// Extract just the version number
		parts := strings.Fields(version)
		if len(parts) > 0 {
			handler.OnCheck("gh", CheckPassed, fmt.Sprintf("gh %s", parts[0]))
		} else {
			handler.OnCheck("gh", CheckPassed, version)
		}
	}

	// Check GitHub authentication
	token, err := getGitHubToken(runner)
	if err != nil {
		warnings++
		handler.OnCheck("github_auth", CheckWarning, "GitHub authentication not configured")
	} else {
		if token == "" {
			warnings++
			handler.OnCheck("github_auth", CheckWarning, "GitHub token is empty")
		} else {
			// Try to create a GitHub client to verify connectivity
			ghCtx := context.Background()
			client, err := github.NewGitHubClient(ghCtx, runner)
			if err != nil {
				warnings++
				handler.OnCheck("github_auth", CheckWarning, fmt.Sprintf("GitHub authentication failed: %v", err))
			} else {
				owner, repo := client.GetOwnerRepo()
				if owner != "" && repo != "" {
					handler.OnCheck("github_auth", CheckPassed, fmt.Sprintf("GitHub authentication successful (%s/%s)", owner, repo))
				} else {
					handler.OnCheck("github_auth", CheckPassed, "GitHub authentication successful")
				}
			}
		}
	}

	return warnings, errors
}
