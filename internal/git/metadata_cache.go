package git

import (
	"sync"
	"sync/atomic"
)

// metadataCache provides thread-safe caching of branch metadata.
// Meta values are immutable, so the cache can safely return shared pointers
// without deep copying.
type metadataCache struct {
	entries sync.Map // map[string]MetadataRecord[*Meta]; value and version stay paired

	// localRefSHAs tracks the same thing for local-metadata refs. Local
	// metadata is not cached (its values are read fresh), but its writes still
	// need something to compare against.
	localRefSHAs sync.Map // map[string]string

	// Instrumentation counters. Updated with atomics to stay lock-free on the
	// hot Get path. Exposed via Summary() for logging and tests.
	hits   atomic.Uint64
	misses atomic.Uint64
}

// MetadataCacheSummary captures cumulative metadata-cache activity since
// process start. Used by tests and the metadata store.
type MetadataCacheSummary struct {
	Hits   uint64
	Misses uint64
}

// Get returns the cached metadata for the given branch, or nil if not cached.
func (c *metadataCache) Get(branchName string) *Meta {
	record, _ := c.GetRecord(branchName)
	return record.Value
}

func (c *metadataCache) GetRecord(branchName string) (MetadataRecord[*Meta], bool) {
	value, ok := c.entries.Load(branchName)
	if !ok {
		c.misses.Add(1)
		return MetadataRecord[*Meta]{}, false
	}
	c.hits.Add(1)
	return value.(MetadataRecord[*Meta]), true
}

// Summary returns the current cumulative hit/miss counts.
func (c *metadataCache) Summary() MetadataCacheSummary {
	return MetadataCacheSummary{
		Hits:   c.hits.Load(),
		Misses: c.misses.Load(),
	}
}

// ResetStats zeroes the instrumentation counters. Intended for tests that
// want to assert behavior of a single operation in isolation.
func (c *metadataCache) ResetStats() {
	c.hits.Store(0)
	c.misses.Store(0)
}

// Put stores the metadata in the cache.
func (c *metadataCache) Put(branchName string, meta *Meta) {
	c.entries.Store(branchName, MetadataRecord[*Meta]{Value: meta})
}

// PutWithSHA stores the metadata along with the ref SHA it was read from, so a
// later write of the same branch can require the ref to still hold that value.
func (c *metadataCache) PutWithSHA(branchName string, meta *Meta, sha string) {
	c.entries.Store(branchName, MetadataRecord[*Meta]{Value: meta, SHA: sha})
}

// SHAFor returns the ref version paired with the cached metadata.
func (c *metadataCache) SHAFor(branchName string) string {
	value, ok := c.entries.Load(branchName)
	if !ok {
		return ""
	}
	return value.(MetadataRecord[*Meta]).SHA
}

// PutLocalSHA records the ref SHA a branch's local metadata was read from.
func (c *metadataCache) PutLocalSHA(branchName, sha string) {
	if sha == "" {
		c.localRefSHAs.Delete(branchName)
		return
	}
	c.localRefSHAs.Store(branchName, sha)
}

// LocalSHAFor returns the local-metadata ref SHA this process last read, or "".
func (c *metadataCache) LocalSHAFor(branchName string) string {
	value, ok := c.localRefSHAs.Load(branchName)
	if !ok {
		return ""
	}
	sha, _ := value.(string)
	return sha
}

// Delete removes the cached metadata for the given branch.
func (c *metadataCache) Delete(branchName string) {
	c.entries.Delete(branchName)
	c.localRefSHAs.Delete(branchName)
}

// Clear removes all entries from the cache.
// Used during Rebuild to ensure stale metadata from external changes is not retained.
func (c *metadataCache) Clear() {
	c.entries.Clear()
	c.localRefSHAs.Clear()
}
