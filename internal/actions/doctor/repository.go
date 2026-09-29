package doctor

import (
	"context"
	"fmt"

	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/git"
)

// repositoryEngine lists the engine methods the repository checks call.
type repositoryEngine interface {
	engine.BranchLookup
	IsInsideRepo() bool
	GetRemoteURL(ctx context.Context) (string, error)
	GetRevision(branch engine.Branch) (string, error)
}

// checkRepository performs repository-related checks
func checkRepository(ctx context.Context, eng repositoryEngine, repoRoot string, handler Handler, warnings int, errors int, trunk string) (int, int) {
	// Check if we're in a git repository
	if repoRoot == "" {
		if !eng.IsInsideRepo() {
			errors++
			handler.OnCheck("git_repo", CheckError, "not in a git repository")
			return warnings, errors
		}
	}
	handler.OnCheck("git_repo", CheckPassed, "Current directory is a git repository")

	// Check remote configuration
	remoteURL, err := eng.GetRemoteURL(ctx)
	if err != nil {
		warnings++
		handler.OnCheck("remote", CheckWarning, "remote 'origin' is not configured")
	} else {
		// Check if it's a GitHub remote
		repo, err := git.ParseRemoteRepository(remoteURL)
		if err != nil || !repo.IsHosted() {
			warnings++
			handler.OnCheck("remote", CheckWarning, "remote 'origin' is not a GitHub repository")
		} else {
			handler.OnCheck("remote", CheckPassed, fmt.Sprintf("Remote 'origin' is configured to GitHub (%s/%s)", repo.Owner, repo.Name))
		}
	}

	// Check trunk branch
	if trunk == "" {
		errors++
		handler.OnCheck("trunk", CheckError, "trunk branch not configured")
	} else {
		// Check if trunk branch exists
		_, err := eng.GetRevision(eng.GetBranch(trunk))
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

	// Local, read-only scan for stale git lock files (a hung operation can leave
	// one behind and block every later git command — see issue #1330).
	warnings = checkGitLocks(repoRoot, handler, warnings)

	return warnings, errors
}
