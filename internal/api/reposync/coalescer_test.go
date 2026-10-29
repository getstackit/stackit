package reposync

import (
	"context"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/getstackit/stackit/internal/api/registry"
	"github.com/stretchr/testify/require"
)

var testRepo = registry.RepoRef{Owner: "o", Name: "r"}

// countingSync returns a SyncFunc that counts its invocations.
func countingSync(calls *atomic.Int32) SyncFunc {
	return func(context.Context, registry.RepoRef) error {
		calls.Add(1)
		return nil
	}
}

func TestCoalescerSingleTriggerRunsOnce(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		var calls atomic.Int32
		c := NewCoalescer(countingSync(&calls), time.Minute, 0)

		c.Trigger(testRepo)
		synctest.Wait()

		require.Equal(t, int32(1), calls.Load())
	})
}

func TestCoalescerCollapsesBurstForSameRepo(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		var calls atomic.Int32
		started := make(chan struct{})
		release := make(chan struct{})

		c := NewCoalescer(func(context.Context, registry.RepoRef) error {
			if calls.Add(1) == 1 {
				close(started)
				<-release // hold the first sync so the burst lands while it runs
			}
			return nil
		}, time.Minute, 0)

		c.Trigger(testRepo)
		<-started // first sync is now in flight

		// A burst of triggers while the first sync runs must collapse to exactly
		// one follow-up, not one sync per trigger.
		for range 5 {
			c.Trigger(testRepo)
		}
		close(release)
		synctest.Wait()

		require.Equal(t, int32(2), calls.Load(), "burst must not produce more than one follow-up sync")
		c.mu.Lock()
		defer c.mu.Unlock()
		require.Empty(t, c.state, "drained repo state must be cleared")
	})
}

func TestCoalescerDebouncesBurstIntoOneSync(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		var calls atomic.Int32
		const debounce = 60 * time.Millisecond
		c := NewCoalescer(countingSync(&calls), time.Minute, debounce)

		// A stack submit's pushes arrive in quick succession. Each trigger lands
		// inside the debounce window and re-arms the timer, so the whole burst
		// collapses into a single sync once the pushes settle.
		for range 5 {
			c.Trigger(testRepo)
			time.Sleep(10 * time.Millisecond) // 10ms < 60ms debounce
		}
		synctest.Wait()
		require.Zero(t, calls.Load(), "sync must wait out the quiet window")

		time.Sleep(debounce)
		synctest.Wait()
		require.Equal(t, int32(1), calls.Load())

		// No stray timer may fire later.
		time.Sleep(time.Hour)
		synctest.Wait()
		require.Equal(t, int32(1), calls.Load(), "debounced burst must collapse to a single sync")
	})
}

func TestCoalescerCoalescesAcrossCasing(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		var calls atomic.Int32
		c := NewCoalescer(countingSync(&calls), time.Minute, 60*time.Millisecond)

		// GitHub repo coordinates are case-insensitive; differently-cased triggers
		// for the same repo must share one debounce window.
		c.Trigger(registry.RepoRef{Owner: "Octo", Name: "Widget"})
		c.Trigger(registry.RepoRef{Owner: "octo", Name: "widget"})

		time.Sleep(time.Hour)
		synctest.Wait()
		require.Equal(t, int32(1), calls.Load(), "differently-cased triggers for one repo must collapse to a single sync")
	})
}

func TestCoalescerRunsDistinctReposConcurrently(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		started := make(chan string, 2)
		release := make(chan struct{})

		c := NewCoalescer(func(_ context.Context, repo registry.RepoRef) error {
			started <- repo.Owner
			<-release // block until both have started, proving they run in parallel
			return nil
		}, time.Minute, 0)

		c.Trigger(registry.RepoRef{Owner: "a", Name: "r"})
		c.Trigger(registry.RepoRef{Owner: "b", Name: "r"})
		synctest.Wait()

		require.Len(t, started, 2, "both repos must start without waiting on each other")
		got := map[string]bool{<-started: true, <-started: true}
		close(release)

		require.Equal(t, map[string]bool{"a": true, "b": true}, got)
	})
}
