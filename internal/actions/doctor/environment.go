package doctor

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/internal/github"
)

// checkEnvironment performs environment-related checks
func checkEnvironment(runner git.Runner, handler Handler, warnings int, errors int) (int, int) {
	// Check git version. The floor is not cosmetic: below it ListWorktrees
	// fails, and every ref-moving command inspects worktrees first — so a git
	// that is merely present but too old leaves the user with a tool that
	// refuses to do anything. Reporting it as passed here is how that turns into
	// a bug report instead of an upgrade.
	gitVersion, err := exec.Command("git", "version").Output()
	switch {
	case err != nil:
		errors++
		handler.OnCheck("git", CheckError, "git is not installed or not in PATH")
	default:
		version := strings.TrimSpace(string(gitVersion))
		major, minor, versionErr := runner.GitVersion(context.Background())
		switch {
		case versionErr != nil:
			warnings++
			handler.OnCheck("git", CheckWarning, fmt.Sprintf("%s (could not determine version: %v)", version, versionErr))
		case !git.AtLeast(major, minor, git.MinGitMajor, git.MinGitMinor):
			errors++
			handler.OnCheck("git", CheckError, fmt.Sprintf(
				"%s is too old; stackit requires Git %d.%d or later. Stack operations will fail until you upgrade.",
				version, git.MinGitMajor, git.MinGitMinor))
		default:
			handler.OnCheck("git", CheckPassed, version)
		}
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

	// Check GitHub authentication. Bound both the token probe and the
	// connectivity check with one short deadline so an unreachable remote is
	// reported quickly rather than hanging this diagnostic.
	ghCtx, cancel := context.WithTimeout(context.Background(), remoteCheckTimeout)
	defer cancel()
	token, err := getGitHubToken(ghCtx, runner)
	if err != nil {
		warnings++
		handler.OnCheck("github_auth", CheckWarning, "GitHub authentication not configured")
	} else {
		if token == "" {
			warnings++
			handler.OnCheck("github_auth", CheckWarning, "GitHub token is empty")
		} else {
			// Try to create a GitHub client to verify connectivity
			client, err := github.NewGitHubClient(ghCtx, runner)
			if err != nil {
				warnings++
				handler.OnCheck("github_auth", CheckWarning, fmt.Sprintf("GitHub authentication failed: %v", err))
			} else {
				repo := client.Repo()
				if repo.Owner != "" && repo.Name != "" {
					handler.OnCheck("github_auth", CheckPassed, fmt.Sprintf("GitHub authentication successful (%s)", repo))
				} else {
					handler.OnCheck("github_auth", CheckPassed, "GitHub authentication successful")
				}
			}
		}
	}

	return warnings, errors
}
