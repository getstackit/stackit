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

	// sharedAbsent and localAbsent mark refs this process itself deleted in a
	// committed transaction. An empty SHA alone means "no expectation" to a
	// standalone write (legacy unconditional behavior); these markers upgrade
	// it to "the ref must not exist", so a ref another process recreated after
	// our delete is not silently overwritten. Any Put or Delete clears them.
	sharedAbsent sync.Map // map[string]struct{}
	localAbsent  sync.Map // map[string]struct{}

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
	c.sharedAbsent.Delete(branchName)
	c.entries.Store(branchName, MetadataRecord[*Meta]{Value: meta})
}

// PutWithSHA stores the metadata along with the ref SHA it was read from, so a
// later write of the same branch can require the ref to still hold that value.
func (c *metadataCache) PutWithSHA(branchName string, meta *Meta, sha string) {
	c.sharedAbsent.Delete(branchName)
	c.entries.Store(branchName, MetadataRecord[*Meta]{Value: meta, SHA: sha})
}

// PutAbsent records that this process deleted the branch's shared metadata
// ref, so a later write requires the ref to still be missing.
func (c *metadataCache) PutAbsent(branchName string) {
	c.entries.Store(branchName, MetadataRecord[*Meta]{Value: NewMeta()})
	c.sharedAbsent.Store(branchName, struct{}{})
}

// IsAbsent reports whether PutAbsent is the latest record for the branch.
func (c *metadataCache) IsAbsent(branchName string) bool {
	_, ok := c.sharedAbsent.Load(branchName)
	return ok
}

// DeleteShared drops only the shared-metadata record for the branch.
func (c *metadataCache) DeleteShared(branchName string) {
	c.entries.Delete(branchName)
	c.sharedAbsent.Delete(branchName)
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
	c.localAbsent.Delete(branchName)
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

// PutLocalAbsent is PutAbsent for the local-metadata ref.
func (c *metadataCache) PutLocalAbsent(branchName string) {
	c.localRefSHAs.Delete(branchName)
	c.localAbsent.Store(branchName, struct{}{})
}

// IsLocalAbsent reports whether PutLocalAbsent is the latest record.
func (c *metadataCache) IsLocalAbsent(branchName string) bool {
	_, ok := c.localAbsent.Load(branchName)
	return ok
}

// Delete removes the cached metadata for the given branch.
func (c *metadataCache) Delete(branchName string) {
	c.entries.Delete(branchName)
	c.localRefSHAs.Delete(branchName)
	c.sharedAbsent.Delete(branchName)
	c.localAbsent.Delete(branchName)
}

// Clear removes all entries from the cache.
// Used during Rebuild to ensure stale metadata from external changes is not retained.
func (c *metadataCache) Clear() {
	c.entries.Clear()
	c.localRefSHAs.Clear()
	c.sharedAbsent.Clear()
	c.localAbsent.Clear()
}
