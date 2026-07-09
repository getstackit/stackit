package git

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// DefaultRemote is the default name for the remote repository
const DefaultRemote = "origin"

// getRemote returns the configured remote for the current branch, falling
// back to DefaultRemote. Implemented via `git config --get` — the value is a
// single string and the call site already accepts a fallback path.
//
// A failure to resolve the current branch (e.g. detached HEAD, corrupted
// repo) is logged at debug level rather than surfaced, because callers want
// a usable remote name; the debug line is the only breadcrumb when an
// unhealthy repo masquerades as a clean "branch with no upstream → origin".
func (r *runner) getRemote() string {
	branch, err := r.GetCurrentBranch()
	if err != nil {
		r.debugLog("getRemote: GetCurrentBranch failed, falling back to %s: %v", DefaultRemote, err)
		return DefaultRemote
	}
	if branch == "" {
		return DefaultRemote
	}
	out, err := r.RunGitCommandWithContext(context.Background(),
		"config", "--get", "branch."+branch+".remote",
	)
	if err == nil {
		if val := strings.TrimSpace(out); val != "" {
			return val
		}
	}
	return DefaultRemote
}

// remoteConfigured reports whether remote has a URL configured. A remote
// name with no URL (e.g. an ephemeral worktree session created without one)
// makes `git fetch <remote>` fail with "does not appear to be a git
// repository" rather than a genuine network/auth error.
func (r *runner) remoteConfigured(ctx context.Context, remote string) bool {
	_, err := r.RunGitCommandWithContext(ctx, "remote", "get-url", remote)
	return err == nil
}

// fetchRemoteShas lists the branch refs on a remote without modifying any
// local refs. `git ls-remote --heads` returns only refs/heads/*.
func (r *runner) fetchRemoteShas(ctx context.Context, remote string) (map[string]string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, DefaultCommandTimeout)
		defer cancel()
	}

	out, err := r.RunGitCommandWithContext(ctx, "ls-remote", "--heads", "--refs", remote)
	if err != nil {
		// Empty remote / no refs is not an error for callers; treat exit
		// code 2 (no matching refs) as an empty result. Network errors
		// surface unchanged.
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 2 {
			return map[string]string{}, nil
		}
		return nil, fmt.Errorf("failed to list remote refs for %s: %w", remote, err)
	}

	result := make(map[string]string)
	for line := range strings.SplitSeq(strings.TrimRight(out, "\n"), "\n") {
		if line == "" {
			continue
		}
		sha, ref, ok := strings.Cut(line, "\t")
		if !ok {
			continue
		}
		// Only branches: refs/heads/<name>
		branchName, ok := strings.CutPrefix(ref, "refs/heads/")
		if !ok {
			continue
		}
		result[branchName] = sha
	}
	return result, nil
}

func (r *runner) getRemoteSha(remote, branchName string) (string, error) {
	out, err := r.RunGitCommandWithContext(context.Background(),
		"rev-parse", "--verify", "--end-of-options",
		fmt.Sprintf("refs/remotes/%s/%s", remote, branchName),
	)
	if err != nil {
		return "", fmt.Errorf("failed to get remote SHA for %s/%s: %w", remote, branchName, err)
	}
	return strings.TrimSpace(out), nil
}
