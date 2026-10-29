package github

import "context"

// GitCommandRunner is the minimal git surface the GitHub integration needs: it
// reads git config (to resolve the host and token) and executes gh CLI
// commands. The full git.Runner satisfies it, so callers pass their existing
// runner; the GitHub package depends only on these two methods rather than the
// whole git surface.
type GitCommandRunner interface {
	GetConfig(ctx context.Context, key string) (string, error)
	RunGH(ctx context.Context, args ...string) (string, error)
}
