package git

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

// MetadataBackend is the blob/ref boundary used by metadata persistence.
// It contains no knowledge of branch or stack metadata schemas.
type MetadataBackend interface {
	ReadObjects(ctx context.Context, refs ...string) (map[string]BatchObject, error)
	CreateBlobs(ctx context.Context, contents ...string) ([]string, error)
	UpdateRefs(ctx context.Context, updates []RefUpdate, message string) error
	DeleteRefs(ctx context.Context, refs ...string) error
	ListRefs(prefix string) (map[string]string, error)
	ReadRevisions(ctx context.Context, refs ...string) ReadResults[string]
	RefGeneration(ref string) uint64
}

// MetadataStore owns metadata serialization, caching, and persistence. Engines
// own a store independently of their Git runner.
type MetadataStore struct {
	git           MetadataBackend
	metadataCache metadataCache
	mu            sync.Mutex
	generations   map[string]uint64
	logger        DebugLogger
}

func NewMetadataStore(backend MetadataBackend) *MetadataStore {
	return &MetadataStore{git: backend, generations: make(map[string]uint64)}
}

func (r *MetadataStore) SetLogger(logger DebugLogger) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.logger = logger
}

func (r *MetadataStore) infoLog(format string, args ...any) {
	if r.logger != nil {
		r.logger.Info(format, args...)
	}
}

// MetadataRecord binds a value to the exact ref version read with it. An empty
// SHA denotes an observed absence, not permission for an unconditional write.
type MetadataRecord[T any] struct {
	Value T
	SHA   string
}

// MetadataReadResults keeps versions alongside values, including absent refs.
// A corrupt record still has a version, allowing a guarded deletion.
type MetadataReadResults[T any] struct {
	ReadResults[T]
	Versions map[string]string
}

func (r MetadataReadResults[T]) Record(name string) (MetadataRecord[T], error) {
	value, err := r.Get(name)
	sha, known := r.Versions[name]
	if err == nil && !known {
		err = fmt.Errorf("no metadata version for %s", name)
	}
	return MetadataRecord[T]{Value: value, SHA: sha}, err
}

// ReadMetadata reads one or many branch records through the shared object reader.
// Results contain successful metadata and per-branch errors.
// Branches that don't have metadata will have an empty Meta struct in the results map.
// Only actual errors (not missing metadata) will be included in the errors map.
func (r *MetadataStore) ReadMetadata(ctx context.Context, branchNames ...string) MetadataReadResults[*Meta] {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := MetadataReadResults[*Meta]{Versions: make(map[string]string, len(branchNames))}
	if err := ctx.Err(); err != nil {
		for _, name := range branchNames {
			result.Fail(name, err)
		}
		return result
	}

	if len(branchNames) == 0 {
		return result
	}

	start := time.Now()

	// Separate cache hits from misses up front.
	var misses []string
	for _, name := range branchNames {
		generation := r.git.RefGeneration(MetadataRefName(name))
		if r.generations[name] != generation {
			r.metadataCache.entries.Delete(name)
		}
		if cached, ok := r.metadataCache.GetRecord(name); ok {
			result.Set(name, cached.Value)
			result.Versions[name] = cached.SHA
		} else {
			misses = append(misses, name)
			r.generations[name] = generation
		}
	}

	if len(misses) == 0 {
		r.infoLog("metadata batch-load kind=shared branches=%d cache_misses=0 elapsed_ms=%d",
			len(branchNames), time.Since(start).Milliseconds())
		return result
	}

	// Build ref names for all cache misses.
	refs := make([]string, len(misses))
	for i, name := range misses {
		refs[i] = fmt.Sprintf("%s%s", MetadataRefPrefix, name)
	}

	// Single burst: all ref lookups + blob reads in one pipe transaction.
	contents, err := r.git.ReadObjects(ctx, refs...)
	if err != nil {
		for _, name := range misses {
			result.Fail(name, err)
		}
		return result
	}

	for i, name := range misses {
		obj := contents[refs[i]] // zero value when the ref is missing
		result.Versions[name] = obj.SHA
		if obj.Content == "" {
			empty := NewMeta()
			r.metadataCache.PutWithSHA(name, empty, obj.SHA)
			result.Set(name, empty)
			continue
		}
		var meta Meta
		if unmarshalErr := json.Unmarshal([]byte(obj.Content), &meta); unmarshalErr != nil {
			result.Fail(name, fmt.Errorf("failed to unmarshal metadata for %s: %w", name, unmarshalErr))
			continue
		}
		// Record the blob this came from so a later WriteMetadata compares
		// against it instead of overwriting blind.
		r.metadataCache.PutWithSHA(name, &meta, obj.SHA)
		result.Set(name, &meta)
	}

	r.infoLog("metadata batch-load kind=shared branches=%d cache_misses=%d elapsed_ms=%d",
		len(branchNames), len(misses), time.Since(start).Milliseconds())

	return result
}

// ReadLocalMetadata reads one or many local metadata records together. Missing
// records return empty metadata; corrupt or unreadable records have per-name
// errors so callers can explicitly choose whether to tolerate them.
func (r *MetadataStore) ReadLocalMetadata(ctx context.Context, branchNames ...string) MetadataReadResults[*LocalMeta] {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := MetadataReadResults[*LocalMeta]{Versions: make(map[string]string)}
	if len(branchNames) == 0 {
		return result
	}
	if err := ctx.Err(); err != nil {
		for _, name := range branchNames {
			result.Fail(name, err)
		}
		return result
	}
	start := time.Now()
	defer func() {
		r.infoLog("metadata batch-load kind=local branches=%d errors=%d elapsed_ms=%d",
			len(branchNames), len(result.Failures()), time.Since(start).Milliseconds())
	}()
	refs := make([]string, len(branchNames))
	for i, name := range branchNames {
		refs[i] = LocalMetadataRefPrefix + name
	}
	contents, err := r.git.ReadObjects(ctx, refs...)
	if err != nil {
		for _, name := range branchNames {
			result.Fail(name, err)
		}
		return result
	}
	for i, name := range branchNames {
		obj := contents[refs[i]]
		result.Versions[name] = obj.SHA
		r.metadataCache.PutLocalSHA(name, obj.SHA)
		if obj.Content == "" {
			result.Set(name, &LocalMeta{})
			continue
		}
		var meta LocalMeta
		if err := json.Unmarshal([]byte(obj.Content), &meta); err != nil {
			result.Fail(name, fmt.Errorf("failed to unmarshal local metadata for %s: %w", name, err))
			continue
		}
		result.Set(name, &meta)
	}
	return result
}

func (r *MetadataStore) WriteMetadata(branchName string, meta *Meta) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	jsonData, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	sha, err := One(r.git.CreateBlobs(context.Background(), string(jsonData)))
	if err != nil {
		return fmt.Errorf("failed to create metadata blob: %w", err)
	}

	refName := fmt.Sprintf("%s%s", MetadataRefPrefix, branchName)
	generation := r.git.RefGeneration(refName)
	// This runner moved the ref (a transaction commit, restack, or undo) since
	// the cached read, so the cached SHA is not what the ref holds. Comparing
	// against it would report another process's change where there was none.
	// Dropping it leaves the expectation unknown, as ReadMetadata does.
	if r.generations[branchName] != generation {
		r.metadataCache.entries.Delete(branchName)
	}
	if err := r.updateMetadataRefCAS(refName, branchName, sha); err != nil {
		return err
	}

	r.metadataCache.PutWithSHA(branchName, meta, sha)
	r.generations[branchName] = generation + 1
	return nil
}

// updateMetadataRefCAS writes a metadata ref, requiring it to still hold the
// blob this process last read.
//
// Without the expectation, two stackit processes in different worktrees that
// both read a branch's metadata and then wrote it would silently keep only the
// second write. Failing is the right outcome: the caller based its new metadata
// on state that no longer exists, so applying it would drop whatever the other
// process recorded.
//
// An unknown expectation (never read in this process, or invalidated since)
// falls back to an unconditional write, which is what creating metadata for a
// newly tracked branch needs.
func (r *MetadataStore) updateMetadataRefCAS(refName, branchName, newSHA string) error {
	expected := r.metadataCache.SHAFor(branchName)
	if expected == "" {
		if err := r.git.UpdateRefs(context.Background(), []RefUpdate{{RefName: refName, NewSHA: newSHA}}, ""); err != nil {
			return fmt.Errorf("failed to write metadata ref: %w", err)
		}
		return nil
	}
	// Writing back what we read is still a write. expected is what *this*
	// process last saw, not what the ref holds now, so short-circuiting here
	// reported success for a ref another process may have moved in between —
	// and the caller then cached metadata it never actually wrote. Let the
	// compare-and-swap decide; it costs one update-ref.
	err := r.git.UpdateRefs(context.Background(), []RefUpdate{{
		RefName: refName,
		NewSHA:  newSHA,
		OldSHA:  expected,
	}}, "")
	if err != nil {
		// The expectation is now known-stale. Dropping it forces the next read
		// to come from disk: otherwise ReadMetadata answers from cache, recomputes
		// the same expectation, and every retry in this process fails the same
		// way — which the short-lived CLI hides but a long-lived server does not.
		r.metadataCache.Delete(branchName)
		return fmt.Errorf("failed to write metadata ref for %s (another process changed it; re-run to pick up their change): %w", branchName, err)
	}
	return nil
}

func (r *MetadataStore) DeleteMetadata(ctx context.Context, branchName string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	refName := fmt.Sprintf("%s%s", MetadataRefPrefix, branchName)
	err := r.git.DeleteRefs(ctx, refName)
	r.metadataCache.Delete(branchName)
	return err
}

// ClearMetadataCache clears the in-memory metadata cache.
// This should be called before a full rebuild to ensure stale entries
// from external changes (e.g., branches created in another terminal) are not retained.
func (r *MetadataStore) ClearMetadataCache() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.metadataCache.Clear()
	clear(r.generations)
}

// MetadataCacheStats returns cumulative hit/miss counts for the metadata cache
// since process start. Counts are read with atomics and are safe to call
// concurrently. Reset via ResetMetadataCacheStats (test-only).
func (r *MetadataStore) MetadataCacheStats() MetadataCacheSummary {
	return r.metadataCache.Summary()
}

// ResetMetadataCacheStats zeroes the metadata cache counters. Intended for
// tests that need to assert behavior of a single operation in isolation.
func (r *MetadataStore) ResetMetadataCacheStats() {
	r.metadataCache.ResetStats()
}

func (r *MetadataStore) RenameMetadata(oldName, newName string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	oldRefName := fmt.Sprintf("%s%s", MetadataRefPrefix, oldName)
	newRefName := fmt.Sprintf("%s%s", MetadataRefPrefix, newName)

	sha, err := r.git.ReadRevisions(context.Background(), oldRefName).One()
	if err != nil {
		return nil //nolint:nilerr // Nothing to rename
	}

	// Copy metadata to new ref (keep old ref for cleanup later)
	if err := r.git.UpdateRefs(context.Background(), []RefUpdate{{RefName: newRefName, NewSHA: sha}}, ""); err != nil {
		return fmt.Errorf("failed to create new metadata ref: %w", err)
	}

	r.metadataCache.Delete(oldName)
	r.metadataCache.Delete(newName)
	return nil
}

func (r *MetadataStore) WriteLocalMetadata(branchName string, meta *LocalMeta) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	jsonData, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("failed to marshal local metadata: %w", err)
	}

	sha, err := One(r.git.CreateBlobs(context.Background(), string(jsonData)))
	if err != nil {
		return fmt.Errorf("failed to create local metadata blob: %w", err)
	}

	refName := fmt.Sprintf("%s%s", LocalMetadataRefPrefix, branchName)
	// Mirrors updateMetadataRefCAS: an expectation we hold is compared even when
	// the new content matches it, because "matches what I read" is not the same
	// as "matches what is on disk now".
	if expected := r.metadataCache.LocalSHAFor(branchName); expected != "" {
		err := r.git.UpdateRefs(context.Background(), []RefUpdate{{
			RefName: refName,
			NewSHA:  sha,
			OldSHA:  expected,
		}}, "")
		if err != nil {
			r.metadataCache.Delete(branchName)
			return fmt.Errorf("failed to write local metadata ref for %s (another process changed it; re-run to pick up their change): %w", branchName, err)
		}
		r.metadataCache.PutLocalSHA(branchName, sha)
		return nil
	}
	if err := r.git.UpdateRefs(context.Background(), []RefUpdate{{RefName: refName, NewSHA: sha}}, ""); err != nil {
		return fmt.Errorf("failed to write local metadata ref: %w", err)
	}
	r.metadataCache.PutLocalSHA(branchName, sha)

	return nil
}

func (r *MetadataStore) ListMetadata() (map[string]string, error) {
	refs, err := r.git.ListRefs(MetadataRefPrefix)
	if err != nil {
		return nil, err
	}

	// Remove prefix from branch names
	result := make(map[string]string)
	for refName, sha := range refs {
		branchName := strings.TrimPrefix(refName, MetadataRefPrefix)
		result[branchName] = sha
	}
	return result, nil
}

// WriteMetadataBlobs marshals each Meta to JSON and writes all the blobs
// in one `git hash-object` invocation via CreateBlobs. Returns SHAs in
// input order. Does NOT update any refs — callers (transaction commit,
// MarkBranchesForPRBodyUpdate) pair the SHAs with ref updates afterwards.
func (r *MetadataStore) WriteMetadataBlobs(ctx context.Context, metas []*Meta) ([]string, error) {
	if len(metas) == 0 {
		return nil, nil
	}
	contents := make([]string, len(metas))
	for i, meta := range metas {
		jsonData, err := json.Marshal(meta)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal metadata at index %d: %w", i, err)
		}
		contents[i] = string(jsonData)
	}
	shas, err := r.git.CreateBlobs(ctx, contents...)
	if err != nil {
		return nil, fmt.Errorf("failed to create metadata blobs: %w", err)
	}
	return shas, nil
}

// WriteLocalMetadataBlobs is the LocalMeta counterpart to
// WriteMetadataBlobs.
func (r *MetadataStore) WriteLocalMetadataBlobs(ctx context.Context, metas []*LocalMeta) ([]string, error) {
	if len(metas) == 0 {
		return nil, nil
	}
	contents := make([]string, len(metas))
	for i, meta := range metas {
		jsonData, err := json.Marshal(meta)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal local metadata at index %d: %w", i, err)
		}
		contents[i] = string(jsonData)
	}
	shas, err := r.git.CreateBlobs(ctx, contents...)
	if err != nil {
		return nil, fmt.Errorf("failed to create local metadata blobs: %w", err)
	}
	return shas, nil
}

// ReadStackMeta reads stack metadata for a given stack ID.
// Returns nil with no error if the stack doesn't exist.
func (r *MetadataStore) ReadStackMeta(stackID string) (*StackMeta, error) {
	refName := StackMetaRefName(stackID)

	// Resolve the ref and read its blob in a single cat-file --batch lookup
	// (which accepts ref names) instead of a rev-parse followed by a blob read.
	objects, err := r.git.ReadObjects(context.Background(), refName)
	object, found := objects[refName]
	content := object.Content
	if err != nil {
		return nil, fmt.Errorf("failed to read stack metadata for %s: %w", stackID, err)
	}
	if !found || content == "" {
		// Ref doesn't exist or is empty: no metadata, not an error.
		return nil, nil
	}

	var meta StackMeta
	if err := json.Unmarshal([]byte(content), &meta); err != nil {
		return nil, fmt.Errorf("failed to unmarshal stack metadata for %s: %w", stackID, err)
	}

	return &meta, nil
}

// WriteStackMeta writes stack metadata for a given stack ID.
func (r *MetadataStore) WriteStackMeta(stackID string, meta *StackMeta) error {
	jsonData, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("failed to marshal stack metadata: %w", err)
	}

	sha, err := One(r.git.CreateBlobs(context.Background(), string(jsonData)))
	if err != nil {
		return fmt.Errorf("failed to create stack metadata blob: %w", err)
	}

	if err := r.git.UpdateRefs(context.Background(), []RefUpdate{{RefName: StackMetaRefName(stackID), NewSHA: sha}}, ""); err != nil {
		return fmt.Errorf("failed to write stack metadata ref: %w", err)
	}

	return nil
}

// DeleteStackMeta deletes stack metadata for a given stack ID.
func (r *MetadataStore) DeleteStackMeta(ctx context.Context, stackID string) error {
	return r.git.DeleteRefs(ctx, StackMetaRefName(stackID))
}

// ListStackMetas returns a map of stack IDs to their ref SHAs.
func (r *MetadataStore) ListStackMetas() (map[string]string, error) {
	refs, err := r.git.ListRefs(StackMetaRefPrefix)
	if err != nil {
		return nil, err
	}

	// Remove prefix from stack IDs
	result := make(map[string]string)
	for refName, sha := range refs {
		stackID := strings.TrimPrefix(refName, StackMetaRefPrefix)
		result[stackID] = sha
	}
	return result, nil
}

// WriteStackMetaBlob creates a blob containing the stack metadata JSON and returns its SHA.
// This does NOT update any refs - use this for batched/transactional writes.
func (r *MetadataStore) WriteStackMetaBlob(meta *StackMeta) (string, error) {
	jsonData, err := json.Marshal(meta)
	if err != nil {
		return "", fmt.Errorf("failed to marshal stack metadata: %w", err)
	}

	sha, err := One(r.git.CreateBlobs(context.Background(), string(jsonData)))
	if err != nil {
		return "", fmt.Errorf("failed to create stack metadata blob: %w", err)
	}

	return sha, nil
}

// GetStackMetaRefSHA returns the current SHA of a stack metadata ref, or empty string if not found.
func (r *MetadataStore) GetStackMetaRefSHA(stackID string) string {
	sha, err := r.git.ReadRevisions(context.Background(), StackMetaRefName(stackID)).One()
	if err != nil {
		return ""
	}
	return sha
}

// WriteBlobs serializes shared, local, or stack records together in input order.
// It creates objects only; the transaction applies the ref updates atomically.
func (r *MetadataStore) WriteBlobs(ctx context.Context, values ...any) ([]string, error) {
	contents := make([]string, len(values))
	for i, value := range values {
		data, err := json.Marshal(value)
		if err != nil {
			return nil, fmt.Errorf("marshal metadata %d: %w", i, err)
		}
		contents[i] = string(data)
	}
	return r.git.CreateBlobs(ctx, contents...)
}
