package main

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"

	"github.com/getstackit/stackit/internal/api/store"
)

// loadReposFromDB reads repo rows from the store and resolves each into a
// registerable repoConfig via normalizeRepoEntry, so DB-sourced repos undergo
// the same path resolution, defaulting, and ID validation the JSON config used
// to. Invalid or duplicate rows are logged and skipped rather than failing
// startup — a single bad row shouldn't take down the whole server.
//
// reposRoot is the resolved checkout root (-repos-root / STACKIT_REPOS_ROOT);
// it is made absolute here, once, before per-entry resolution.
func loadReposFromDB(ctx context.Context, st *store.Store, reposRoot string) ([]repoConfig, error) {
	if reposRoot != "" {
		abs, err := filepath.Abs(reposRoot)
		if err != nil {
			return nil, fmt.Errorf("repos-root %q: %w", reposRoot, err)
		}
		reposRoot = abs
	}

	rows, err := st.ListRepos(ctx)
	if err != nil {
		return nil, err
	}

	out := make([]repoConfig, 0, len(rows))
	seen := make(map[string]bool, len(rows))
	for _, r := range rows {
		rc := repoConfig{
			ID:          r.ID,
			DisplayName: r.DisplayName,
			Owner:       r.Owner,
			Name:        r.Name,
			Path:        r.Path,
			Remote:      r.Remote,
			AddedBy:     r.AddedBy,
		}
		if err := normalizeRepoEntry(&rc, reposRoot); err != nil {
			slog.Warn("repo skipped (invalid config)", "repo", r.ID, "error", err)
			continue
		}
		if !repoIDPattern.MatchString(rc.ID) {
			slog.Warn("repo skipped (invalid id)", "id", rc.ID)
			continue
		}
		if seen[rc.ID] {
			slog.Warn("repo skipped (duplicate id)", "id", rc.ID)
			continue
		}
		seen[rc.ID] = true
		out = append(out, rc)
	}
	return out, nil
}
