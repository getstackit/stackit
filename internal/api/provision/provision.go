// Package provision builds registry entries from repo coordinates, resolving
// the per-repo engine and GitHub client through the app bootstrap layer. It is
// the shared path used by both server startup and runtime repo onboarding, so
// the two can't drift in how they wire an engine, broadcaster, and ref watcher.
package provision

import (
	"context"
	"log/slog"

	"github.com/getstackit/stackit/internal/api/registry"
	"github.com/getstackit/stackit/internal/app"
)

// EntryParams is the resolved input needed to build a registry entry: a
// URL-safe ID, a display label, the on-disk checkout Path, and the git Remote
// name. Coordinate resolution (owner/name → <reposRoot>/<owner>/<name>) happens
// in the caller, before BuildEntry.
type EntryParams struct {
	ID          string
	DisplayName string
	Path        string
	Remote      string
}

// BuildEntry resolves p into a runtime context via app.GetContext and returns a
// ready registry.RepoEntry — broadcaster and ref watcher started, logger close
// hook attached. The caller adds it to the registry. An empty Path falls back
// to git discovery from the process working directory, matching the single-repo
// startup shortcut.
func BuildEntry(ctx context.Context, p EntryParams) (*registry.RepoEntry, error) {
	opts := app.GetDefaultGlobalOptions()
	opts.Cwd = p.Path
	opts.Interactive = false

	runtimeCtx, err := app.GetContext(ctx, opts)
	if err != nil {
		return nil, err
	}

	gh := runtimeCtx.GitHub()
	if gh == nil && runtimeCtx.GitHubError() != nil {
		slog.Warn("GitHub client unavailable", "repo", p.ID, "error", runtimeCtx.GitHubError())
	}

	entry := registry.NewEntry(registry.EntryConfig{
		ID:          p.ID,
		DisplayName: p.DisplayName,
		RepoRoot:    runtimeCtx.RepoRoot,
		Remote:      p.Remote,
		Engine:      runtimeCtx.Engine,
		GitHub:      gh,
	})
	if runtimeCtx.Logger != nil {
		entry.AddCloser(runtimeCtx.Logger.Close)
	}

	return entry, nil
}
