package doctor

import (
	"fmt"

	"stackit.dev/stackit/internal/app"
	"stackit.dev/stackit/internal/github"
)

// checkRepository performs repository-related checks
func checkRepository(ctx *app.Context, handler Handler, warnings int, errors int, trunk string) (int, int) {
	// Check if we're in a git repository
	if ctx.RepoRoot == "" {
		if !ctx.Engine.Git().IsInsideRepo() {
			errors++
			handler.OnCheck("git_repo", CheckError, "not in a git repository")
			return warnings, errors
		}
	}
	handler.OnCheck("git_repo", CheckPassed, "Current directory is a git repository")

	// Check remote configuration
	remoteURL, err := ctx.Engine.GetRemoteURL(ctx.Context)
	if err != nil {
		warnings++
		handler.OnCheck("remote", CheckWarning, "remote 'origin' is not configured")
	} else {
		// Check if it's a GitHub remote
		repoInfo, err := github.ParseGitHubRemoteURL(remoteURL)
		if err != nil {
			warnings++
			handler.OnCheck("remote", CheckWarning, "remote 'origin' is not a GitHub repository")
		} else {
			handler.OnCheck("remote", CheckPassed, fmt.Sprintf("Remote 'origin' is configured to GitHub (%s/%s)", repoInfo.Owner, repoInfo.Repo))
		}
	}

	// Check trunk branch
	if trunk == "" {
		errors++
		handler.OnCheck("trunk", CheckError, "trunk branch not configured")
	} else {
		// Check if trunk branch exists
		_, err := ctx.Engine.GetRevision(ctx.Engine.GetBranch(trunk))
		if err != nil {
			errors++
			handler.OnCheck("trunk", CheckError, fmt.Sprintf("trunk branch '%s' does not exist", trunk))
		} else {
			handler.OnCheck("trunk", CheckPassed, fmt.Sprintf("Trunk branch '%s' exists", trunk))
		}
	}

	// Check if stackit is initialized (if trunk is set, it's initialized)
	if trunk == "" {
		errors++
		handler.OnCheck("initialized", CheckError, "stackit is not initialized (run 'stackit init')")
	} else {
		handler.OnCheck("initialized", CheckPassed, "stackit is initialized")
	}

	return warnings, errors
}
