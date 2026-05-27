package git

import "sync"

// revisionCache provides thread-safe caching of branch SHA revisions.
// This avoids respawning `git rev-parse` for every revision lookup.
// Keyed by branch name, values are full SHA hex strings.
type revisionCache struct {
	entries sync.Map // map[string]string (branchName -> SHA)
}

// Get returns the cached revision for the given branch, or empty string if not cached.
func (c *revisionCache) Get(branchName string) (string, bool) {
	value, ok := c.entries.Load(branchName)
	if !ok {
		return "", false
	}
	sha, ok := value.(string)
	return sha, ok
}

// Put stores the revision in the cache.
func (c *revisionCache) Put(branchName string, sha string) {
	c.entries.Store(branchName, sha)
}

// Delete removes the cached revision for the given branch.
func (c *revisionCache) Delete(branchName string) {
	c.entries.Delete(branchName)
}

// InvalidateAll clears all cached revisions.
// Used after bulk mutation operations (rebase, reset, etc.).
func (c *revisionCache) InvalidateAll() {
	c.entries.Clear()
}
