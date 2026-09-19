package git

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMetadataCache_GetPut(t *testing.T) {
	t.Parallel()

	var c metadataCache

	// Miss returns nil
	require.Nil(t, c.Get("main"))

	// Put then hit
	meta := NewMeta().WithLockReason(LockReasonUser)
	c.Put("main", meta)
	got := c.Get("main")
	require.NotNil(t, got)
	require.Equal(t, LockReasonUser, got.GetLockReason())

	// Overwrite
	meta2 := NewMeta().WithLockReason(LockReasonConsolidating)
	c.Put("main", meta2)
	got = c.Get("main")
	require.Equal(t, LockReasonConsolidating, got.GetLockReason())
}

func TestMetadataCache_Delete(t *testing.T) {
	t.Parallel()

	var c metadataCache
	c.Put("feature", NewMeta())

	c.Delete("feature")
	require.Nil(t, c.Get("feature"))

	// Delete non-existent key is a no-op
	c.Delete("nonexistent")
}

func TestMetadataCache_Clear(t *testing.T) {
	t.Parallel()

	var c metadataCache
	c.Put("feature-a", NewMeta())
	c.Put("feature-b", NewMeta())
	c.Put("feature-c", NewMeta())

	c.Clear()

	require.Nil(t, c.Get("feature-a"))
	require.Nil(t, c.Get("feature-b"))
	require.Nil(t, c.Get("feature-c"))
}

func TestMetadataCache_Concurrent(t *testing.T) {
	t.Parallel()

	var c metadataCache
	var wg sync.WaitGroup
	const goroutines = 50

	// Concurrent Put/Get/Delete should not panic
	wg.Add(goroutines)
	for range goroutines {
		go func() {
			defer wg.Done()
			key := "branch"
			c.Put(key, NewMeta())
			c.Get(key)
			c.Delete(key)
		}()
	}
	wg.Wait()
}

func TestMetadataCache_HitMissCounters(t *testing.T) {
	t.Parallel()

	var c metadataCache

	// Initial state: zero hits, zero misses.
	require.Equal(t, MetadataCacheSummary{}, c.Summary())

	// A miss bumps misses but not hits.
	require.Nil(t, c.Get("missing"))
	require.Equal(t, MetadataCacheSummary{Hits: 0, Misses: 1}, c.Summary())

	// Populate, then a hit bumps hits but not misses.
	c.Put("present", NewMeta())
	require.NotNil(t, c.Get("present"))
	require.Equal(t, MetadataCacheSummary{Hits: 1, Misses: 1}, c.Summary())

	// Multiple hits accumulate.
	for range 4 {
		require.NotNil(t, c.Get("present"))
	}
	require.Equal(t, MetadataCacheSummary{Hits: 5, Misses: 1}, c.Summary())

	// ResetStats zeroes counters without disturbing entries.
	c.ResetStats()
	require.Equal(t, MetadataCacheSummary{}, c.Summary())
	require.NotNil(t, c.Get("present"))
	require.Equal(t, MetadataCacheSummary{Hits: 1, Misses: 0}, c.Summary())
}

func TestMetadataCache_CountersConcurrent(t *testing.T) {
	t.Parallel()

	var c metadataCache
	c.Put("present", NewMeta())

	var wg sync.WaitGroup
	const goroutines = 32
	const itersPerG = 100

	wg.Add(goroutines)
	for i := range goroutines {
		go func(i int) {
			defer wg.Done()
			for j := range itersPerG {
				if (i+j)%2 == 0 {
					c.Get("present") // hit
				} else {
					c.Get("missing") // miss
				}
			}
		}(i)
	}
	wg.Wait()

	got := c.Summary()
	require.Equal(t, uint64(goroutines*itersPerG), got.Hits+got.Misses,
		"every Get must increment exactly one counter; race-free atomics")
}
