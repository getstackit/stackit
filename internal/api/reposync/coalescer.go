package reposync

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"
)

// defaultSyncTimeout bounds a single coalesced sync so a stuck remote can't
// pin a goroutine indefinitely.
const defaultSyncTimeout = 2 * time.Minute

// SyncFunc mirror-fetches and refreshes one repo by its GitHub coordinates.
// (*Syncer).SyncRepo satisfies it.
type SyncFunc func(ctx context.Context, owner, name string) error

// Coalescer collapses repeated sync triggers for the same repo. At most one
// sync runs per repo at a time; triggers that arrive while one is in flight set
// a single pending flag, so a burst of pushes for a repo yields one extra sync
// afterward rather than a fetch per delivery. This bounds live goroutines to the
// number of distinct repos seeing traffic and keeps git subprocesses in check —
// the webhook receiver can hand off every delivery without fanning out.
type Coalescer struct {
	sync    SyncFunc
	timeout time.Duration

	mu    sync.Mutex
	state map[string]*repoSync
}

// repoSync tracks the in-flight/pending status for one repo key.
type repoSync struct {
	running bool
	pending bool
}

// NewCoalescer wraps sync. A non-positive timeout uses defaultSyncTimeout.
func NewCoalescer(sync SyncFunc, timeout time.Duration) *Coalescer {
	if timeout <= 0 {
		timeout = defaultSyncTimeout
	}
	return &Coalescer{sync: sync, timeout: timeout, state: make(map[string]*repoSync)}
}

// Trigger requests a sync for owner/name and returns immediately. It is safe for
// concurrent use; repeated calls while a sync for the repo is in flight coalesce
// into a single follow-up run.
func (c *Coalescer) Trigger(owner, name string) {
	key := owner + "/" + name

	c.mu.Lock()
	st := c.state[key]
	if st == nil {
		st = &repoSync{}
		c.state[key] = st
	}
	if st.running {
		// A sync is already running for this repo; mark that the latest state
		// hasn't been synced yet. Any number of triggers in this window collapse
		// to one follow-up.
		st.pending = true
		c.mu.Unlock()
		return
	}
	st.running = true
	c.mu.Unlock()

	go c.drain(key, owner, name)
}

// drain runs syncs for key until no trigger arrived during the last run.
func (c *Coalescer) drain(key, owner, name string) {
	for {
		ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
		err := c.sync(ctx, owner, name)
		cancel()
		switch {
		case err == nil:
			// Success is logged at the sync chokepoint (reposync syncEntry).
		case errors.Is(err, ErrRepoNotManaged):
			// Expected, not a failure: the GitHub App webhook delivers a push
			// for every repo the App is installed on, so we routinely see
			// pushes for repos this server doesn't serve. Note it and move on.
			slog.Info("sync: ignoring push for repo not managed here", "repo", key)
		default:
			slog.Warn("sync: coalesced sync failed", "repo", key, "error", err)
		}

		c.mu.Lock()
		st := c.state[key]
		if st.pending {
			// A trigger landed while we were syncing; run once more to pick it
			// up, then re-check.
			st.pending = false
			c.mu.Unlock()
			continue
		}
		delete(c.state, key)
		c.mu.Unlock()
		return
	}
}
