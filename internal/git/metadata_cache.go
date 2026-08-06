package git

import (
	"strings"
	"sync"
	"sync/atomic"
)

// metadataCache provides thread-safe caching of branch metadata.
// Meta values are immutable, so the cache can safely return shared pointers
// without deep copying.
type metadataCache struct {
	entries sync.Map // map[string]*Meta
	refSHAs sync.Map // map[string]string — ref SHA each entry was read from

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
// process start. Used by tests and by the runner to log periodic stats.
type MetadataCacheSummary struct {
	Hits   uint64
	Misses uint64
}

// Get returns the cached metadata for the given branch, or nil if not cached.
func (c *metadataCache) Get(branchName string) *Meta {
	value, ok := c.entries.Load(branchName)
	if !ok {
		c.misses.Add(1)
		return nil
	}
	c.hits.Add(1)
	meta, _ := value.(*Meta)
	return meta
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
	c.entries.Store(branchName, meta)
}

// PutWithSHA stores the metadata along with the ref SHA it was read from, so a
// later write of the same branch can require the ref to still hold that value.
func (c *metadataCache) PutWithSHA(branchName string, meta *Meta, sha string) {
	c.entries.Store(branchName, meta)
	if sha == "" {
		c.refSHAs.Delete(branchName)
		return
	}
	c.refSHAs.Store(branchName, sha)
}

// SHAFor returns the ref SHA this process last read for the branch, or "" when
// it has not read one. Writers treat "" as "no expectation to enforce" rather
// than "the ref is absent", because the branch may simply never have been read
// in this process.
func (c *metadataCache) SHAFor(branchName string) string {
	value, ok := c.refSHAs.Load(branchName)
	if !ok {
		return ""
	}
	sha, _ := value.(string)
	return sha
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
	c.refSHAs.Delete(branchName)
	c.localRefSHAs.Delete(branchName)
}

// Clear removes all entries from the cache.
// Used during Rebuild to ensure stale metadata from external changes is not retained.
func (c *metadataCache) Clear() {
	c.entries.Clear()
	c.refSHAs.Clear()
	c.localRefSHAs.Clear()
}

// InvalidateForRefs clears cached metadata for any refs in the given batch
// that are metadata refs. This ensures batch operations (transactions)
// don't leave stale cache entries.
func (c *metadataCache) InvalidateForRefs(updates []RefUpdate) {
	for _, update := range updates {
		if branchName, ok := strings.CutPrefix(update.RefName, MetadataRefPrefix); ok {
			c.Delete(branchName)
		}
		if branchName, ok := strings.CutPrefix(update.RefName, LocalMetadataRefPrefix); ok {
			c.localRefSHAs.Delete(branchName)
		}
	}
}

// InvalidateForRefNames clears cached metadata for any ref names that
// are metadata refs. Used by DeleteRefsBatch which operates on raw ref names.
func (c *metadataCache) InvalidateForRefNames(refNames []string) {
	for _, refName := range refNames {
		if branchName, ok := strings.CutPrefix(refName, MetadataRefPrefix); ok {
			c.Delete(branchName)
		}
		if branchName, ok := strings.CutPrefix(refName, LocalMetadataRefPrefix); ok {
			c.localRefSHAs.Delete(branchName)
		}
	}
}
