package reposync

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/getstackit/stackit/internal/api/registry"
)

// defaultSyncTimeout bounds a single coalesced sync so a stuck remote can't
// pin a goroutine indefinitely.
const defaultSyncTimeout = 2 * time.Minute

// defaultDebounce is the quiet window a repo's triggers wait out before syncing.
// Submitting a stack pushes its branches in quick succession, each firing a
// webhook; debouncing collapses that sequence into a single fetch once the
// pushes settle, instead of one fetch per branch.
const defaultDebounce = 2 * time.Second

// SyncFunc mirror-fetches and refreshes one repo by its GitHub coordinates.
// (*Syncer).SyncRepo satisfies it.
type SyncFunc func(ctx context.Context, repo registry.RepoRef) error

// Coalescer debounces and collapses repeated sync triggers for the same repo.
// A trigger (re)arms a per-repo timer; the sync runs only after the timer's
// quiet window elapses with no newer trigger, so a burst of pushes — e.g. a
// whole stack submit — yields a single fetch once they settle. At most one sync
// runs per repo at a time; a trigger that lands while one is in flight sets a
// pending flag for exactly one follow-up. This bounds live goroutines to the
// number of distinct repos seeing traffic and keeps git subprocesses in check —
// the webhook receiver can hand off every delivery without fanning out.
type Coalescer struct {
	sync     SyncFunc
	timeout  time.Duration
	debounce time.Duration

	mu    sync.Mutex
	state map[string]*repoSync
}

// repoSync tracks the debounce timer and in-flight/pending status for one repo
// key. gen disambiguates a just-fired timer from a fresher one re-armed by a
// trigger that raced it, so at most one run() proceeds per key.
type repoSync struct {
	running bool
	pending bool
	gen     uint64
	timer   *time.Timer
}

// NewCoalescer wraps sync. A non-positive timeout uses defaultSyncTimeout; a
// negative debounce uses defaultDebounce, while a zero debounce dispatches
// immediately (no quiet window).
func NewCoalescer(sync SyncFunc, timeout, debounce time.Duration) *Coalescer {
	if timeout <= 0 {
		timeout = defaultSyncTimeout
	}
	if debounce < 0 {
		debounce = defaultDebounce
	}
	return &Coalescer{sync: sync, timeout: timeout, debounce: debounce, state: make(map[string]*repoSync)}
}

// Trigger requests a sync for repo and returns immediately. It is safe for
// concurrent use. The sync is debounced: a burst of triggers keeps bumping the
// timer out and collapses into one run once the quiet window elapses; triggers
// that land while a sync is already running coalesce into a single follow-up.
func (c *Coalescer) Trigger(repo registry.RepoRef) {
	key := repo.Key()

	c.mu.Lock()
	defer c.mu.Unlock()

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
		return
	}

	// (Re)arm the debounce timer. Bumping gen and stopping any prior timer means
	// a timer that already fired (its run() not yet past the lock) sees a stale
	// gen and bows out, so only the latest timer's run() proceeds.
	st.gen++
	gen := st.gen
	if st.timer != nil {
		st.timer.Stop()
	}
	st.timer = time.AfterFunc(c.debounce, func() { c.run(key, repo, gen) })
}

// run executes syncs for key until no trigger arrived during the last run. It
// is the debounce timer's callback; gen guards against a superseded timer.
func (c *Coalescer) run(key string, repo registry.RepoRef, gen uint64) {
	c.mu.Lock()
	st := c.state[key]
	if st == nil || st.gen != gen || st.running {
		// A newer trigger re-armed the timer (and will fire its own run), or a
		// run is already active for this key. This fire is stale; drop it.
		c.mu.Unlock()
		return
	}
	st.running = true
	st.timer = nil
	c.mu.Unlock()

	for {
		ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
		err := c.sync(ctx, repo)
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
