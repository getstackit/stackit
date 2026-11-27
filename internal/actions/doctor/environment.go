package doctor

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/internal/github"
)

// remoteCheckTimeout bounds doctor's GitHub connectivity probes. Doctor is a
// diagnostic, so a fast "unreachable" beats blocking for minutes on the git
// runner's default timeout when the network or `gh` is wedged.
const remoteCheckTimeout = 10 * time.Second

// gitVersioner reports the installed Git's version.
type gitVersioner interface {
	GitVersion(ctx context.Context) (git.Version, error)
}

// checkEnvironment performs environment-related checks. versioner supplies the
// installed Git version; ghRunner runs the gh CLI probes.
func checkEnvironment(ctx context.Context, versioner gitVersioner, ghRunner github.GitCommandRunner, handler Handler, warnings int, errors int) (int, int) {
	// Check git version. The floor is not cosmetic: below it ListWorktrees
	// fails, and every ref-moving command inspects worktrees first — so a git
	// that is merely present but too old leaves the user with a tool that
	// refuses to do anything. Reporting it as passed here is how that turns into
	// a bug report instead of an upgrade.
	gitVersion, err := exec.CommandContext(ctx, "git", "version").Output()
	switch {
	case err != nil:
		errors++
		handler.OnCheck("git", CheckError, "git is not installed or not in PATH")
	default:
		version := strings.TrimSpace(string(gitVersion))
		installed, versionErr := versioner.GitVersion(ctx)
		switch {
		case versionErr != nil:
			warnings++
			handler.OnCheck("git", CheckWarning, fmt.Sprintf("%s (could not determine version: %v)", version, versionErr))
		case !installed.AtLeast(git.MinimumGitVersion):
			errors++
			handler.OnCheck("git", CheckError, fmt.Sprintf(
				"%s is too old; stackit requires Git %d.%d or later. Stack operations will fail until you upgrade.",
				version, git.MinimumGitVersion.Major, git.MinimumGitVersion.Minor))
		default:
			handler.OnCheck("git", CheckPassed, version)
		}
	}

	// Check gh CLI
	ghVersion, err := exec.CommandContext(ctx, "gh", "version").Output()
	if err != nil {
		warnings++
		handler.OnCheck("gh", CheckWarning, "GitHub CLI (gh) is not installed or not in PATH")
	} else {
		handler.OnCheck("gh", CheckPassed, formatGHVersion(string(ghVersion)))
	}

	// Check GitHub authentication. Bound both the token probe and the
	// connectivity check with one short deadline so an unreachable remote is
	// reported quickly rather than hanging this diagnostic.
	ghCtx, cancel := context.WithTimeout(ctx, remoteCheckTimeout)
	defer cancel()
	token, err := github.Token(ghCtx, ghRunner)
	if err != nil {
		warnings++
		handler.OnCheck("github_auth", CheckWarning, "GitHub authentication not configured")
	} else {
		if token == "" {
			warnings++
			handler.OnCheck("github_auth", CheckWarning, "GitHub token is empty")
		} else {
			// Try to create a GitHub client to verify connectivity
			client, err := github.NewGitHubClient(ghCtx, ghRunner)
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

// formatGHVersion condenses `gh version` output to "gh <version>". The output
// is "gh version 2.89.0 (2026-03-26)" followed by a release URL line, so the
// version number is the third field of the first line, not the first. Output
// in any other shape is reported as its trimmed first line.
func formatGHVersion(out string) string {
	firstLine, _, _ := strings.Cut(strings.TrimSpace(out), "\n")
	firstLine = strings.TrimSpace(firstLine)
	fields := strings.Fields(firstLine)
	if len(fields) >= 3 && fields[0] == "gh" && fields[1] == "version" {
		return "gh " + fields[2]
	}
	return firstLine
}
