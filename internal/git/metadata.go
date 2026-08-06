package git

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// LockReason is an enum for the reason why a branch is locked
type LockReason string

const (
	// LockReasonNone indicates the branch is not locked
	LockReasonNone LockReason = ""
	// LockReasonUser indicates the branch was manually locked by the user
	LockReasonUser LockReason = "user"
	// LockReasonConsolidating indicates the branch is being consolidated
	LockReasonConsolidating LockReason = "consolidating"
	// LockReasonDraining indicates the branch is being drained (merge drain in progress)
	LockReasonDraining LockReason = "draining"
)

// IsLocked returns true if the lock reason indicates the branch is locked
func (r LockReason) IsLocked() bool {
	return r != LockReasonNone
}

// BranchType indicates the type of branch
type BranchType string

// Branch types
const (
	BranchTypeUser           BranchType = "user"            // Normal stacked branch
	BranchTypeUtility        BranchType = "utility"         // Created by st merge --consolidate or other internal tasks
	BranchTypeWorktreeAnchor BranchType = "worktree-anchor" // Anchor branch for worktree, has no commits
)

// Meta represents branch metadata stored in Git refs.
// Fields are unexported to enforce immutability — use getters to read
// and With* methods to create modified copies. Construct via NewMeta()
// or NewMetaFrom(MetaFields{...}).
type Meta struct {
	parentBranchName     *string
	parentBranchRevision *string
	prInfo               *PrInfoPersistence
	scope                *string
	lockReason           LockReason

	// Fields for remote sync
	branchType     BranchType
	lastModifiedBy *ModifiedBy
	lastModifiedAt *time.Time
	localOnlyHash  *string

	// mergedDownstack preserves historical parent relationships when branches are reparented
	// due to merge/deletion. Ordered oldest to newest, limited to 5 entries max.
	mergedDownstack []MergedParent

	// stackID links this branch to a stack ref (refs/stackit/stacks/{stack-id}).
	stackID *string
}

// StackDescription holds stack-level title and description.
// This is stored on the root branch of a stack.
type StackDescription struct {
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
}

// IsEmpty returns true if both title and description are empty.
func (sd *StackDescription) IsEmpty() bool {
	return sd == nil || (sd.Title == "" && sd.Description == "")
}

// PRState is a GitHub pull-request state as reported by the API
// (GraphQL uppercase form). Empty means unknown.
type PRState string

const (
	PRStateOpen   PRState = "OPEN"
	PRStateMerged PRState = "MERGED"
	PRStateClosed PRState = "CLOSED"
)

// MergedParent represents a historical parent that was merged or deleted
type MergedParent struct {
	BranchName string   `json:"branchName"`
	PRNumber   *int     `json:"prNumber,omitempty"`
	PRState    *PRState `json:"prState,omitempty"` // MERGED or CLOSED
}

// LocalMeta represents branch metadata that is strictly local and never pushed
type LocalMeta struct {
	Frozen              bool   `json:"frozen,omitempty"`
	NeedsPRBodyUpdate   bool   `json:"needsPRBodyUpdate,omitempty"`
	NavigationCommentID *int64 `json:"navigationCommentId,omitempty"`
}

// LocalMetaMap is branch name -> local metadata, as returned by the batch
// local-metadata readers.
type LocalMetaMap map[string]*LocalMeta

// Get returns the local metadata for a branch, or nil if absent.
// Safe to call on a nil map.
func (m LocalMetaMap) Get(branchName string) *LocalMeta {
	return m[branchName]
}

// ModifiedBy represents information about who last modified the metadata
type ModifiedBy struct {
	GitName        string  `json:"gitName"`
	GitEmail       string  `json:"gitEmail"`
	GitHubUsername *string `json:"githubUsername,omitempty"`
}

// PrInfoPersistence represents PR information for persistence
type PrInfoPersistence struct {
	Number      *int        `json:"number,omitempty"`
	Base        *string     `json:"base,omitempty"`
	BaseSHA     *string     `json:"baseSHA,omitempty"`
	URL         *string     `json:"url,omitempty"`
	Title       *string     `json:"title,omitempty"`
	Body        *string     `json:"body,omitempty"`
	State       *PRState    `json:"state,omitempty"`
	IsDraft     *bool       `json:"isDraft,omitempty"`
	LockReason  *LockReason `json:"lockReason,omitempty"`
	MergeBranch *string     `json:"mergeBranch,omitempty"`
}

const (
	// MetadataRefPrefix is the prefix for Git refs where branch metadata is stored
	MetadataRefPrefix = "refs/stackit/metadata/"
	// LocalMetadataRefPrefix is the prefix for Git refs where local-only branch metadata is stored
	LocalMetadataRefPrefix = "refs/stackit/local-metadata/"
)

// BatchReadMetadata reads metadata for multiple branches in parallel.
// Returns two maps: one with successfully read metadata and one with errors for failed reads.
// Branches that don't have metadata will have an empty Meta struct in the results map.
// Only actual errors (not missing metadata) will be included in the errors map.
func (r *runner) BatchReadMetadata(branchNames []string) (map[string]*Meta, map[string]error) {
	results := make(map[string]*Meta, len(branchNames))
	errs := make(map[string]error)

	if len(branchNames) == 0 {
		return results, errs
	}

	start := time.Now()

	// Separate cache hits from misses up front.
	var misses []string
	for _, name := range branchNames {
		if cached := r.metadataCache.Get(name); cached != nil {
			results[name] = cached
		} else {
			misses = append(misses, name)
		}
	}

	if len(misses) == 0 {
		r.infoLog("metadata batch-load kind=shared branches=%d cache_misses=0 elapsed_ms=%d",
			len(branchNames), time.Since(start).Milliseconds())
		return results, errs
	}

	// Build ref names for all cache misses.
	refs := make([]string, len(misses))
	for i, name := range misses {
		refs[i] = fmt.Sprintf("%s%s", MetadataRefPrefix, name)
	}

	// Single burst: all ref lookups + blob reads in one pipe transaction.
	contents, err := r.objects.ReadObjectsBatch(refs)
	if err != nil {
		for _, name := range misses {
			errs[name] = err
		}
		return results, errs
	}

	for i, name := range misses {
		content := contents[refs[i]] // empty string when the ref is missing
		if content == "" {
			empty := NewMeta()
			r.metadataCache.Put(name, empty)
			results[name] = empty
			continue
		}
		var meta Meta
		if unmarshalErr := json.Unmarshal([]byte(content), &meta); unmarshalErr != nil {
			errs[name] = fmt.Errorf("failed to unmarshal metadata for %s: %w", name, unmarshalErr)
			continue
		}
		r.metadataCache.Put(name, &meta)
		results[name] = &meta
	}

	r.infoLog("metadata batch-load kind=shared branches=%d cache_misses=%d elapsed_ms=%d",
		len(branchNames), len(misses), time.Since(start).Milliseconds())

	return results, errs
}

// BatchReadLocalMetadata reads local metadata for multiple branches in parallel.
// Returns a map of successfully read metadata. Failures are silently ignored since
// local metadata is not critical and missing metadata is expected for new branches.
func (r *runner) BatchReadLocalMetadata(branchNames []string) LocalMetaMap {
	results := make(LocalMetaMap, len(branchNames))

	if len(branchNames) == 0 {
		return results
	}

	start := time.Now()

	refs := make([]string, len(branchNames))
	for i, name := range branchNames {
		refs[i] = fmt.Sprintf("%s%s", LocalMetadataRefPrefix, name)
	}

	// Single burst for all local metadata refs.
	contents, err := r.objects.ReadObjectsBatch(refs)
	if err != nil {
		// Local metadata is non-critical; fall back to empty results on error.
		r.infoLog("metadata batch-load kind=local branches=%d error=%v elapsed_ms=%d",
			len(branchNames), err, time.Since(start).Milliseconds())
		return results
	}

	for i, name := range branchNames {
		content := contents[refs[i]]
		if content == "" {
			results[name] = &LocalMeta{}
			continue
		}
		var meta LocalMeta
		if err := json.Unmarshal([]byte(content), &meta); err != nil {
			// Non-critical; treat as missing
			results[name] = &LocalMeta{}
			continue
		}
		results[name] = &meta
	}

	r.infoLog("metadata batch-load kind=local branches=%d elapsed_ms=%d",
		len(branchNames), time.Since(start).Milliseconds())

	return results
}

func (r *runner) ReadMetadata(branchName string) (*Meta, error) {
	// Check cache first to avoid redundant git process spawns.
	// Meta is immutable, so returning the cached pointer is safe.
	if cached := r.metadataCache.Get(branchName); cached != nil {
		return cached, nil
	}

	refName := fmt.Sprintf("%s%s", MetadataRefPrefix, branchName)

	// Pass the ref name directly to cat-file --batch: it resolves ref → blob in one
	// step, replacing the two rev-parse subprocesses GetRef previously spawned.
	content, sha, found, err := r.objects.ReadObjectWithSHA(refName)
	if err != nil {
		return nil, fmt.Errorf("failed to read metadata for %s: %w", branchName, err)
	}
	if !found || content == "" {
		empty := NewMeta()
		r.metadataCache.Put(branchName, empty)
		return empty, nil
	}

	var meta Meta
	if err := json.Unmarshal([]byte(content), &meta); err != nil {
		return nil, fmt.Errorf("failed to unmarshal metadata for %s: %w", branchName, err)
	}

	// Remember which blob this came from so WriteMetadata can refuse to
	// overwrite a different one.
	r.metadataCache.PutWithSHA(branchName, &meta, sha)
	return &meta, nil
}

func (r *runner) WriteMetadata(branchName string, meta *Meta) error {
	jsonData, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	sha, err := r.CreateBlob(string(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create metadata blob: %w", err)
	}

	refName := fmt.Sprintf("%s%s", MetadataRefPrefix, branchName)
	if err := r.updateMetadataRefCAS(refName, branchName, sha); err != nil {
		return err
	}

	r.metadataCache.PutWithSHA(branchName, meta, sha)
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
func (r *runner) updateMetadataRefCAS(refName, branchName, newSHA string) error {
	expected := r.metadataCache.SHAFor(branchName)
	if expected == "" {
		if err := r.UpdateRef(refName, newSHA); err != nil {
			return fmt.Errorf("failed to write metadata ref: %w", err)
		}
		return nil
	}
	if expected == newSHA {
		return nil // Identical content; nothing to write.
	}
	err := r.UpdateRefsBatch(context.Background(), []RefUpdate{{
		RefName: refName,
		NewSHA:  newSHA,
		OldSHA:  expected,
	}})
	if err != nil {
		return fmt.Errorf("failed to write metadata ref for %s (another process changed it; re-run to pick up their change): %w", branchName, err)
	}
	return nil
}

func (r *runner) DeleteMetadata(ctx context.Context, branchName string) error {
	refName := fmt.Sprintf("%s%s", MetadataRefPrefix, branchName)
	err := r.DeleteRef(ctx, refName)
	r.metadataCache.Delete(branchName)
	return err
}

// ClearMetadataCache clears the in-memory metadata cache.
// This should be called before a full rebuild to ensure stale entries
// from external changes (e.g., branches created in another terminal) are not retained.
func (r *runner) ClearMetadataCache() {
	r.metadataCache.Clear()
}

// MetadataCacheStats returns cumulative hit/miss counts for the metadata cache
// since process start. Counts are read with atomics and are safe to call
// concurrently. Reset via ResetMetadataCacheStats (test-only).
func (r *runner) MetadataCacheStats() MetadataCacheSummary {
	return r.metadataCache.Summary()
}

// ResetMetadataCacheStats zeroes the metadata cache counters. Intended for
// tests that need to assert behavior of a single operation in isolation.
func (r *runner) ResetMetadataCacheStats() {
	r.metadataCache.ResetStats()
}

func (r *runner) RenameMetadata(oldName, newName string) error {
	oldRefName := fmt.Sprintf("%s%s", MetadataRefPrefix, oldName)
	newRefName := fmt.Sprintf("%s%s", MetadataRefPrefix, newName)

	sha, err := r.GetRef(oldRefName)
	if err != nil {
		return nil //nolint:nilerr // Nothing to rename
	}

	// Copy metadata to new ref (keep old ref for cleanup later)
	if err := r.UpdateRef(newRefName, sha); err != nil {
		return fmt.Errorf("failed to create new metadata ref: %w", err)
	}

	r.metadataCache.Delete(oldName)
	r.metadataCache.Delete(newName)
	return nil
}

func (r *runner) ReadLocalMetadata(branchName string) (*LocalMeta, error) {
	refName := fmt.Sprintf("%s%s", LocalMetadataRefPrefix, branchName)

	content, sha, found, err := r.objects.ReadObjectWithSHA(refName)
	if err != nil {
		return nil, fmt.Errorf("failed to read local metadata for %s: %w", branchName, err)
	}
	if !found || content == "" {
		return &LocalMeta{}, nil
	}
	// Remember the blob so WriteLocalMetadata can refuse to clobber a different one.
	r.metadataCache.PutLocalSHA(branchName, sha)

	var meta LocalMeta
	if err := json.Unmarshal([]byte(content), &meta); err != nil {
		return nil, fmt.Errorf("failed to unmarshal local metadata for %s: %w", branchName, err)
	}

	return &meta, nil
}

func (r *runner) WriteLocalMetadata(branchName string, meta *LocalMeta) error {
	jsonData, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("failed to marshal local metadata: %w", err)
	}

	sha, err := r.CreateBlob(string(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create local metadata blob: %w", err)
	}

	refName := fmt.Sprintf("%s%s", LocalMetadataRefPrefix, branchName)
	if expected := r.metadataCache.LocalSHAFor(branchName); expected != "" && expected != sha {
		err := r.UpdateRefsBatch(context.Background(), []RefUpdate{{
			RefName: refName,
			NewSHA:  sha,
			OldSHA:  expected,
		}})
		if err != nil {
			return fmt.Errorf("failed to write local metadata ref for %s (another process changed it; re-run to pick up their change): %w", branchName, err)
		}
		r.metadataCache.PutLocalSHA(branchName, sha)
	} else if err := r.UpdateRef(refName, sha); err != nil {
		return fmt.Errorf("failed to write local metadata ref: %w", err)
	} else {
		r.metadataCache.PutLocalSHA(branchName, sha)
	}

	return nil
}

func (r *runner) ListMetadata() (map[string]string, error) {
	refs, err := r.ListRefs(MetadataRefPrefix)
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

// WriteMetadataBlobsBatch marshals each Meta to JSON and writes all the blobs
// in one `git hash-object` invocation via CreateBlobsBatch. Returns SHAs in
// input order. Does NOT update any refs — callers (transaction commit,
// MarkBranchesForPRBodyUpdate) pair the SHAs with ref updates afterwards.
func (r *runner) WriteMetadataBlobsBatch(ctx context.Context, metas []*Meta) ([]string, error) {
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
	shas, err := r.CreateBlobsBatch(ctx, contents)
	if err != nil {
		return nil, fmt.Errorf("failed to create metadata blobs: %w", err)
	}
	return shas, nil
}

// WriteLocalMetadataBlobsBatch is the LocalMeta counterpart to
// WriteMetadataBlobsBatch.
func (r *runner) WriteLocalMetadataBlobsBatch(ctx context.Context, metas []*LocalMeta) ([]string, error) {
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
	shas, err := r.CreateBlobsBatch(ctx, contents)
	if err != nil {
		return nil, fmt.Errorf("failed to create local metadata blobs: %w", err)
	}
	return shas, nil
}

// GetMetadataRefSHA returns the current SHA of a metadata ref, or empty string if not found.
func (r *runner) GetMetadataRefSHA(branchName string) string {
	refName := fmt.Sprintf("%s%s", MetadataRefPrefix, branchName)
	sha, err := r.GetRef(refName)
	if err != nil {
		return ""
	}
	return sha
}

// GetLocalMetadataRefSHA returns the current SHA of a local metadata ref, or empty string if not found.
func (r *runner) GetLocalMetadataRefSHA(branchName string) string {
	refName := fmt.Sprintf("%s%s", LocalMetadataRefPrefix, branchName)
	sha, err := r.GetRef(refName)
	if err != nil {
		return ""
	}
	return sha
}

// MetadataRefName returns the full ref name for a branch's metadata.
func MetadataRefName(branchName string) string {
	return fmt.Sprintf("%s%s", MetadataRefPrefix, branchName)
}

// LocalMetadataRefName returns the full ref name for a branch's local metadata.
func LocalMetadataRefName(branchName string) string {
	return fmt.Sprintf("%s%s", LocalMetadataRefPrefix, branchName)
}
