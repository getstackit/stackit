package reposync

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCoalescerSingleTriggerRunsOnce(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	calls := 0
	c := NewCoalescer(func(context.Context, string, string) error {
		mu.Lock()
		calls++
		mu.Unlock()
		return nil
	}, time.Minute, 0)

	c.Trigger("o", "r")
	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return calls == 1
	}, 2*time.Second, 5*time.Millisecond)
}

func TestCoalescerCollapsesBurstForSameRepo(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	calls := 0
	started := make(chan struct{})
	release := make(chan struct{})

	c := NewCoalescer(func(context.Context, string, string) error {
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		if n == 1 {
			close(started)
			<-release // hold the first sync so the burst lands while it runs
		}
		return nil
	}, time.Minute, 0)

	c.Trigger("o", "r")
	<-started // first sync is now in flight

	// A burst of triggers while the first sync runs must collapse to exactly
	// one follow-up, not one sync per trigger.
	for range 5 {
		c.Trigger("o", "r")
	}
	close(release)

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return calls == 2
	}, 2*time.Second, 5*time.Millisecond)

	// Once drained, the state for the repo is cleared and the count is stable.
	require.Eventually(t, func() bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		return len(c.state) == 0
	}, 2*time.Second, 5*time.Millisecond)

	mu.Lock()
	require.Equal(t, 2, calls, "burst must not produce more than one follow-up sync")
	mu.Unlock()
}

func TestCoalescerDebouncesBurstIntoOneSync(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	calls := 0
	c := NewCoalescer(func(context.Context, string, string) error {
		mu.Lock()
		calls++
		mu.Unlock()
		return nil
	}, time.Minute, 60*time.Millisecond)

	// A stack submit's pushes arrive in quick succession. Each trigger lands
	// inside the debounce window and re-arms the timer, so the whole burst
	// collapses into a single sync once the pushes settle.
	for range 5 {
		c.Trigger("o", "r")
		time.Sleep(10 * time.Millisecond) // 10ms < 60ms debounce
	}

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return calls == 1
	}, 2*time.Second, 5*time.Millisecond)

	// Give any stray timer ample time to fire; the count must stay at one.
	time.Sleep(150 * time.Millisecond)
	mu.Lock()
	require.Equal(t, 1, calls, "debounced burst must collapse to a single sync")
	mu.Unlock()
}

func TestCoalescerRunsDistinctReposConcurrently(t *testing.T) {
	t.Parallel()
	started := make(chan string, 2)
	release := make(chan struct{})

	c := NewCoalescer(func(_ context.Context, owner, _ string) error {
		started <- owner
		<-release // block until both have started, proving they run in parallel
		return nil
	}, time.Minute, 0)

	c.Trigger("a", "r")
	c.Trigger("b", "r")

	got := map[string]bool{}
	for range 2 {
		select {
		case owner := <-started:
			got[owner] = true
		case <-time.After(2 * time.Second):
			t.Fatal("both repos did not start concurrently")
		}
	}
	close(release)

	require.Equal(t, map[string]bool{"a": true, "b": true}, got)
}
