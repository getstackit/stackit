package github

import "context"

// GitCommandRunner is the minimal git surface the GitHub integration needs: it
// reads git config (to resolve the host and token) and executes gh CLI
// commands. The full git.Runner satisfies it, so callers pass their existing
// runner; the GitHub package depends only on these two methods rather than the
// whole git surface.
type GitCommandRunner interface {
	GetConfig(key string) (string, error)
	RunGHCommandWithContext(ctx context.Context, args ...string) (string, error)
}
