package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/getstackit/stackit/internal/api/store/reposdb"
)

// ErrRepoNotFound is returned by GetRepo when no row matches the given ID.
var ErrRepoNotFound = errors.New("repo not found")

// Repo is a persisted repo-configuration row. Fields mirror the server's
// repoConfig minus reposRoot, which is a server-level resolution input rather
// than a per-repo property: keeping it out of the row lets two hosts point the
// same table at different checkout roots. Path is the RAW stored value (may be
// empty, relative, or owner/name-derivable); the server resolves it after
// loading via normalizeRepoEntry.
type Repo struct {
	ID          string
	DisplayName string
	Owner       string
	Name        string
	Path        string
	Remote      string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func repoFromRow(r reposdb.Repo) Repo {
	return Repo{
		ID:          r.ID,
		DisplayName: r.DisplayName,
		Owner:       r.Owner,
		Name:        r.Name,
		Path:        r.Path,
		Remote:      r.Remote,
		CreatedAt:   r.CreatedAt,
		UpdatedAt:   r.UpdatedAt,
	}
}

// ListRepos returns all stored repos ordered by ID.
func (s *Store) ListRepos(ctx context.Context) ([]Repo, error) {
	rows, err := s.queries.ListRepos(ctx)
	if err != nil {
		return nil, fmt.Errorf("list repos: %w", err)
	}
	repos := make([]Repo, 0, len(rows))
	for _, row := range rows {
		repos = append(repos, repoFromRow(row))
	}
	return repos, nil
}

// GetRepo returns the repo with the given ID, or ErrRepoNotFound.
func (s *Store) GetRepo(ctx context.Context, id string) (Repo, error) {
	row, err := s.queries.GetRepo(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Repo{}, ErrRepoNotFound
		}
		return Repo{}, fmt.Errorf("get repo %q: %w", id, err)
	}
	return repoFromRow(row), nil
}

// AddRepo inserts r and returns the stored row with its server-assigned
// timestamps. The caller is responsible for validating r.ID (see repoIDPattern
// in apps/server) before calling.
func (s *Store) AddRepo(ctx context.Context, r Repo) (Repo, error) {
	row, err := s.queries.InsertRepo(ctx, reposdb.InsertRepoParams{
		ID:          r.ID,
		DisplayName: r.DisplayName,
		Owner:       r.Owner,
		Name:        r.Name,
		Path:        r.Path,
		Remote:      r.Remote,
	})
	if err != nil {
		return Repo{}, fmt.Errorf("add repo %q: %w", r.ID, err)
	}
	return repoFromRow(row), nil
}

// DeleteRepo removes the repo with the given ID. Deleting a missing row is a
// no-op (no error), matching DELETE semantics.
func (s *Store) DeleteRepo(ctx context.Context, id string) error {
	if err := s.queries.DeleteRepo(ctx, id); err != nil {
		return fmt.Errorf("delete repo %q: %w", id, err)
	}
	return nil
}
