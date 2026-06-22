package provision

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// MirrorFetch updates a server-managed checkout to mirror its remote: it
// force-updates local branch heads and stackit metadata refs from the remote
// and prunes refs deleted upstream, so the served stack graph and stack
// descriptions reflect GitHub.
//
// The checkout's HEAD is detached first because git refuses to fetch into the
// currently checked-out branch; a managed mirror has no working branch, and the
// server reads refs (not the working tree), so a frozen tree is harmless.
//
// token authenticates the fetch — a GitHub App installation token for private
// repos; empty fetches without auth (public repos).
func MirrorFetch(ctx context.Context, repoRoot, remote, token string) error {
	if remote == "" {
		remote = "origin"
	}
	if err := detachHead(ctx, repoRoot); err != nil {
		return err
	}

	cmd := exec.CommandContext(ctx, "git", "-C", repoRoot, "fetch", "--prune", remote,
		"+refs/heads/*:refs/heads/*",
		"+refs/stackit/metadata/*:refs/stackit/metadata/*",
		"+refs/stackit/stacks/*:refs/stackit/stacks/*",
	)
	cmd.Env = gitEnv(token)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git mirror-fetch %s: %w: %s", remote, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// detachHead detaches the checkout's HEAD when it is on a branch, so a mirror
// fetch can update every refs/heads/*. It is a no-op when HEAD is already
// detached (the steady state after the first fetch).
func detachHead(ctx context.Context, repoRoot string) error {
	// symbolic-ref -q HEAD exits 0 (and prints the branch) when on a branch,
	// non-zero when already detached — already detached means nothing to do.
	onBranch := exec.CommandContext(ctx, "git", "-C", repoRoot, "symbolic-ref", "-q", "HEAD").Run() == nil
	if !onBranch {
		return nil
	}
	cmd := exec.CommandContext(ctx, "git", "-C", repoRoot, "checkout", "--detach")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("detach HEAD: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
