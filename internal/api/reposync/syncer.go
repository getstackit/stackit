// Package reposync keeps server-managed repo checkouts in step with their
// remotes. On an interval it mirror-fetches each managed registry entry — using
// a GitHub App installation token for auth — and triggers the entry's refresh
// so connected clients see the update, with no local actor required.
package reposync

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/getstackit/stackit/internal/api/provision"
	"github.com/getstackit/stackit/internal/api/registry"
	"github.com/getstackit/stackit/internal/utils"
)

// ErrRepoNotManaged is returned by SyncRepo when no managed registry entry
// matches the requested owner/name — e.g. a webhook arrives for a repo that
// was never onboarded, or for the unmanaged -cwd dev repo.
var ErrRepoNotManaged = errors.New("reposync: repo not managed")

// defaultWorkers bounds concurrent fetches per pass. Fetches are network-bound,
// so a small pool is gentle on the remote and the file-descriptor table.
const defaultWorkers = 4

// TokenProvider mints a credential for fetching owner/name. The concrete
// implementation is *github.AppTokenProvider; a nil provider fetches without
// auth (public repos only).
type TokenProvider interface {
	InstallationToken(ctx context.Context, owner, name string) (string, error)
}

// Syncer mirror-fetches managed registry entries on an interval.
type Syncer struct {
	reg      *registry.Registry
	provider TokenProvider
	interval time.Duration
	workers  int

	// Seams, defaulting to the real implementations, so tests can drive the
	// loop without git or a network.
	fetch   func(ctx context.Context, repoRoot, remote, token string) error
	refresh func(e *registry.RepoEntry)
}

// New builds a Syncer. provider may be nil (no GitHub App configured), in which
// case fetches carry no auth and only public repos refresh.
func New(reg *registry.Registry, provider TokenProvider, interval time.Duration) *Syncer {
	return &Syncer{
		reg:      reg,
		provider: provider,
		interval: interval,
		workers:  defaultWorkers,
		fetch:    provision.MirrorFetch,
		refresh:  (*registry.RepoEntry).Refresh,
	}
}

// Run mirror-fetches all managed entries every interval until ctx is canceled.
// It blocks; start it in a goroutine.
func (s *Syncer) Run(ctx context.Context) {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.syncOnce(ctx)
		}
	}
}

// syncOnce fetches every managed entry once, with bounded concurrency. The
// registry is re-read each pass, so repos onboarded after start join the loop.
func (s *Syncer) syncOnce(ctx context.Context) {
	var managed []*registry.RepoEntry
	for _, e := range s.reg.List() {
		if e.Managed {
			managed = append(managed, e)
		}
	}
	if len(managed) == 0 {
		return
	}
	utils.RunWithWorkers(managed, s.workers, func(e *registry.RepoEntry) {
		if ctx.Err() != nil {
			return
		}
		if err := s.syncEntry(ctx, e); err != nil {
			slog.Warn("sync: entry failed", "repo", e.ID, "error", err)
		}
	})
}

// SyncRepo mirror-fetches and refreshes the single managed entry matching
// owner/name. It is the per-repo unit of work shared by the interval loop and
// the webhook receiver: the loop calls syncEntry directly over the entries it
// already holds, while the webhook path arrives with only GitHub coordinates
// and resolves the entry here. Returns ErrRepoNotManaged when no managed repo
// matches, so a webhook for an un-onboarded repo is a clean no-op.
func (s *Syncer) SyncRepo(ctx context.Context, owner, name string) error {
	e, ok := s.reg.FindManaged(owner, name)
	if !ok {
		return fmt.Errorf("%w: %s/%s", ErrRepoNotManaged, owner, name)
	}
	return s.syncEntry(ctx, e)
}

func (s *Syncer) syncEntry(ctx context.Context, e *registry.RepoEntry) error {
	token := ""
	if s.provider != nil {
		t, err := s.provider.InstallationToken(ctx, e.Owner, e.Name)
		if err != nil {
			return fmt.Errorf("installation token: %w", err)
		}
		token = t
	}
	if err := s.fetch(ctx, e.RepoRoot, e.Remote, token); err != nil {
		return fmt.Errorf("fetch: %w", err)
	}
	s.refresh(e)
	// The single success line for every sync path — interval loop, manual sync,
	// and webhook-triggered. For a webhook push it follows the handler's
	// "webhook: push accepted" line, completing the receive→refresh trace.
	slog.Info("sync: refreshed repo", "repo", e.ID, "owner", e.Owner, "name", e.Name)
	return nil
}
