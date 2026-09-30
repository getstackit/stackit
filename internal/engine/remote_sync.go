package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/getstackit/stackit/internal/git"
)

// RemoteMetadataView provides read-only access to the remote metadata cache.
// It is a lightweight snapshot — no map copying is needed because the underlying
// map is replaced atomically on each LoadRemoteMetadataCache call.
type RemoteMetadataView struct {
	entries map[string]*git.Meta
}

// Get returns the remote metadata for a branch, or nil if not present.
func (v RemoteMetadataView) Get(branch string) *git.Meta {
	return v.entries[branch]
}

// Has returns true if the branch has remote metadata.
func (v RemoteMetadataView) Has(branch string) bool {
	_, ok := v.entries[branch]
	return ok
}

// Range iterates over all entries. The callback receives each branch name and its metadata.
// Return false from the callback to stop iteration.
func (v RemoteMetadataView) Range(fn func(branch string, meta *git.Meta) bool) {
	for k, m := range v.entries {
		if !fn(k, m) {
			return
		}
	}
}

// Len returns the number of entries in the cache.
func (v RemoteMetadataView) Len() int {
	return len(v.entries)
}

// IsRemoteSyncEnabled checks if metadata compatibility has been verified and sync is enabled
func (e *engineImpl) IsRemoteSyncEnabled() bool {
	val, err := e.git.GetConfig("stackit.metadata-sync-enabled")
	return err == nil && val == "true"
}

// setRemoteSyncEnabled marks metadata compatibility as verified and enables/disables sync
func (e *engineImpl) setRemoteSyncEnabled(enabled bool) {
	val := "false"
	if enabled {
		val = "true"
	}
	_ = e.git.SetConfig("stackit.metadata-sync-enabled", val)
}

// BatchSetLastModifiedBy stamps the current git user onto metadata for
// multiple branches with a single git config lookup and a single atomic
// metadata commit.
func (e *engineImpl) BatchSetLastModifiedBy(ctx context.Context, branchNames []string) error {
	if len(branchNames) == 0 {
		return nil
	}

	// Fetch git config once
	name, err := e.git.GetConfig("user.name")
	if err != nil {
		return fmt.Errorf("git user.name is required but not set: %w", err)
	}
	email, _ := e.git.GetConfig("user.email")

	modifiedBy := &git.ModifiedBy{
		GitName:  name,
		GitEmail: email,
	}
	now := time.Now()

	message := fmt.Sprintf("set last modified by for %d branches", len(branchNames))
	return e.updateMetadataBatch(ctx, message, branchNames, func(_ string, meta *git.Meta) *git.Meta {
		return meta.WithLastModifiedBy(modifiedBy).WithLastModifiedAt(&now)
	})
}

// updateMetadataBatch rewrites each branch's metadata with apply and commits
// the results in one retried transaction. The localOnlyHash is refreshed so
// change detection treats the write as the new synced state.
//
// A branch whose metadata cannot be read fails the batch rather than being
// overwritten: a missing ref reads as empty metadata, so a read failure means
// a corrupt record whose parent would be lost.
func (e *engineImpl) updateMetadataBatch(
	ctx context.Context,
	message string,
	branchNames []string,
	apply func(branch string, meta *git.Meta) *git.Meta,
) error {
	return e.WithRetry(ctx, func() error {
		tx := e.BeginTx(message)
		metas, readErrs := tx.ReadMetadata(ctx, branchNames...).ValuesAndErrors()
		if len(readErrs) > 0 {
			tx.Rollback()
			return fmt.Errorf("read metadata: %w", errors.Join(readErrs...))
		}
		for _, branchName := range branchNames {
			meta := apply(branchName, metas[branchName])
			hash := e.computeMetadataHash(meta)
			if err := tx.UpdateMeta(branchName, meta.WithLocalOnlyHash(&hash)); err != nil {
				tx.Rollback()
				return err
			}
		}
		return tx.Commit(ctx)
	})
}

// remoteMetadataRefPrefix is where fetched remote metadata refs live.
const remoteMetadataRefPrefix = "refs/stackit/remote-metadata/"

// LoadRemoteMetadataCache loads remote metadata refs into the engine's cache
// with one batched object read.
func (e *engineImpl) LoadRemoteMetadataCache(ctx context.Context) error {
	remoteRefs, err := e.git.ListRefs(remoteMetadataRefPrefix)
	if err != nil {
		return err
	}

	cache := make(MetaMap, len(remoteRefs))
	if len(remoteRefs) > 0 {
		// Read by ref name: names are unique where blob SHAs may repeat, and a
		// ref deleted since the listing is simply absent from the result.
		objects, err := e.git.ReadObjects(ctx, slices.Collect(maps.Keys(remoteRefs))...)
		if err != nil {
			return fmt.Errorf("read remote metadata: %w", err)
		}
		for refName, object := range objects {
			var meta git.Meta
			// An unparseable remote record is treated as absent.
			if err := json.Unmarshal([]byte(object.Content), &meta); err != nil {
				continue
			}
			cache[strings.TrimPrefix(refName, remoteMetadataRefPrefix)] = &meta
		}
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	e.remoteMetaCache = cache
	return nil
}

// applyRemoteMetadataIfExists applies remote metadata to a local branch if it exists in the cache
func (e *engineImpl) applyRemoteMetadataIfExists(ctx context.Context, branchName string) error {
	e.mu.RLock()
	remote, ok := e.remoteMetaCache[branchName]
	e.mu.RUnlock()
	if !ok {
		return nil
	}
	return e.applyRemoteMetadata(ctx, MetaMap{branchName: remote})
}

// ApplyRemoteMetadataForBranches applies the latest fetched remote metadata to
// the requested local branches in a single atomic metadata commit. It owns
// the cache/refspec setup so callers don't need to sequence
// ConfigureRemoteMetadataSync, LoadRemoteMetadataCache, and per-branch
// application themselves.
func (e *engineImpl) ApplyRemoteMetadataForBranches(ctx context.Context, branchNames []string) error {
	if len(branchNames) == 0 {
		return nil
	}

	if err := e.ConfigureRemoteMetadataSync(ctx); err != nil {
		return fmt.Errorf("configure metadata sync: %w", err)
	}
	if err := e.LoadRemoteMetadataCache(ctx); err != nil {
		return fmt.Errorf("load remote metadata cache: %w", err)
	}

	trunkName := e.Trunk().GetName()
	remotes := make(MetaMap, len(branchNames))

	e.mu.RLock()
	for _, branchName := range branchNames {
		if branchName == "" || branchName == trunkName {
			continue
		}
		if remote, ok := e.remoteMetaCache[branchName]; ok {
			remotes[branchName] = remote
		}
	}
	e.mu.RUnlock()

	return e.applyRemoteMetadata(ctx, remotes)
}

// applyRemoteMetadata overlays each branch's remote metadata onto its local
// metadata in one commit. The commit refreshes in-memory branch state, so lock
// and scope changes are visible without a rebuild.
func (e *engineImpl) applyRemoteMetadata(ctx context.Context, remotes MetaMap) error {
	if len(remotes) == 0 {
		return nil
	}
	branchNames := slices.Sorted(maps.Keys(remotes))
	message := fmt.Sprintf("apply remote metadata for %d branches", len(branchNames))
	return e.updateMetadataBatch(ctx, message, branchNames, func(branch string, local *git.Meta) *git.Meta {
		return mergeRemoteMetadata(local, remotes[branch])
	})
}

// mergeRemoteMetadata returns local metadata with remote's syncable fields
// applied on top, preserving local-only fields (like PrInfo) that remote
// metadata does not carry.
func mergeRemoteMetadata(local, remote *git.Meta) *git.Meta {
	return local.
		WithLockReason(remote.GetLockReason()).
		WithScope(remote.GetScope()).
		WithBranchType(remote.GetBranchType()).
		WithLastModifiedBy(remote.GetLastModifiedBy()).
		WithLastModifiedAt(remote.GetLastModifiedAt())
}

// GetRemoteMetadataCache returns a read-only view of the remote metadata cache.
// Returns RemoteMetadataView instead of a raw map to prevent external mutation
// and provide a stable snapshot even if the cache is reloaded concurrently.
// Use Get/Has/Range methods to access entries.
func (e *engineImpl) GetRemoteMetadataCache() RemoteMetadataView {
	e.mu.RLock()
	defer e.mu.RUnlock()

	// Snapshot the map reference — the map itself is replaced atomically
	// in LoadRemoteMetadataCache, so this reference is stable.
	return RemoteMetadataView{entries: e.remoteMetaCache}
}

// ComputeMetadataDiff compares local and remote metadata for a branch
func (e *engineImpl) ComputeMetadataDiff(branch string) (*MetadataDiff, error) {
	e.mu.RLock()
	local, err := e.readMetadata(branch)
	remote := e.remoteMetaCache[branch]
	e.mu.RUnlock()

	if err != nil {
		return nil, err
	}

	return buildMetadataDiff(branch, local, remote), nil
}

// buildMetadataDiff compares already-loaded local and remote metadata for a
// branch. Returns nil if there is no remote metadata to diff against.
func buildMetadataDiff(branch string, local, remote *git.Meta) *MetadataDiff {
	if remote == nil {
		return nil // No remote metadata, nothing to diff
	}

	diff := &MetadataDiff{
		Branch:     branch,
		LocalMeta:  local,
		RemoteMeta: remote,
	}

	// Compare syncable fields
	if local.GetLockReason() != remote.GetLockReason() {
		diff.Differences = append(diff.Differences, FieldDiff{
			Field:       "lockReason",
			LocalValue:  local.GetLockReason(),
			RemoteValue: remote.GetLockReason(),
		})
	}

	localScope := ""
	if local.GetScope() != nil {
		localScope = *local.GetScope()
	}
	remoteScope := ""
	if remote.GetScope() != nil {
		remoteScope = *remote.GetScope()
	}

	if localScope != remoteScope {
		diff.Differences = append(diff.Differences, FieldDiff{
			Field:       "scope",
			LocalValue:  localScope,
			RemoteValue: remoteScope,
		})
	}

	diff.HasConflict = len(diff.Differences) > 0
	return diff
}

// ComputeAllMetadataDiffs computes diffs for all branches in the remote cache that exist locally
func (e *engineImpl) ComputeAllMetadataDiffs() ([]*MetadataDiff, error) {
	e.mu.RLock()
	// Filter to only include branches that exist locally (as git branches)
	localBranches := make(map[string]bool)
	for _, b := range e.state.branches {
		localBranches[b] = true
	}

	branches := make([]string, 0, len(e.remoteMetaCache))
	remotes := make(map[string]*git.Meta, len(e.remoteMetaCache))
	for b, remote := range e.remoteMetaCache {
		if localBranches[b] {
			branches = append(branches, b)
			remotes[b] = remote
		}
	}
	e.mu.RUnlock()

	locals, metaErrs := e.batchReadMetadata(branches)

	var diffs []*MetadataDiff
	for _, branch := range branches {
		if err := metaErrs[branch]; err != nil {
			return nil, err
		}
		diff := buildMetadataDiff(branch, locals[branch], remotes[branch])
		if diff != nil && diff.HasConflict {
			diffs = append(diffs, diff)
		}
	}
	return diffs, nil
}

// AcceptRemoteMetadata overwrites local metadata with remote values
func (e *engineImpl) AcceptRemoteMetadata(ctx context.Context, branch string) error {
	return e.applyRemoteMetadataIfExists(ctx, branch)
}

// RejectRemoteMetadata marks a branch as having local modifications to keep
func (e *engineImpl) RejectRemoteMetadata(branch string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if state := e.readState(branch); state != nil {
		state.LocalModified = true
	}
}

// HasLocalModifications checks if a branch has local metadata changes that differ from its original fetched state
func (e *engineImpl) HasLocalModifications(branch string) bool {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if state := e.readState(branch); state != nil && state.LocalModified {
		return true
	}

	local, err := e.readMetadata(branch)
	if err != nil || local.GetLocalOnlyHash() == nil {
		return false // Never synced or error, treat as not modified
	}

	currentHash := e.computeMetadataHash(local)
	return currentHash != *local.GetLocalOnlyHash()
}

// computeMetadataHash calculates a hash of the syncable fields for change detection
func (e *engineImpl) computeMetadataHash(meta *git.Meta) string {
	// Hash of lockReason + scope (fields user can modify locally)
	scope := ""
	if meta.GetScope() != nil {
		scope = *meta.GetScope()
	}
	data := fmt.Sprintf("lockReason:%s,scope:%s", string(meta.GetLockReason()), scope)
	hash := sha256.Sum256([]byte(data))
	return hex.EncodeToString(hash[:])
}

// MetadataDiff represents the differences between local and remote metadata
type MetadataDiff struct {
	Branch      string
	LocalMeta   *git.Meta
	RemoteMeta  *git.Meta
	Differences []FieldDiff
	HasConflict bool
}

// FieldDiff represents a difference in a single metadata field
type FieldDiff struct {
	Field       string
	LocalValue  any
	RemoteValue any
}

// OrphanedMetadataAction represents the action to take for orphaned metadata
type OrphanedMetadataAction string

const (
	// OrphanedActionDelete indicates the local metadata should be deleted
	OrphanedActionDelete OrphanedMetadataAction = "delete"
	// OrphanedActionPrompt indicates the user should be prompted
	OrphanedActionPrompt OrphanedMetadataAction = "prompt"
)

// OrphanedMetadataInfo contains information about orphaned local metadata
type OrphanedMetadataInfo struct {
	BranchName      string
	Action          OrphanedMetadataAction
	HasLocalChanges bool
	ExistsLocally   bool
	LocalMeta       *git.Meta
}

// FindOrphanedLocalMetadata identifies branches that have local metadata but no corresponding local branch or remote metadata.
// This handles scenarios where branches were deleted elsewhere or manually via git.
func (e *engineImpl) FindOrphanedLocalMetadata() ([]OrphanedMetadataInfo, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	// List all local metadata refs
	localRefs, err := e.git.ListRefs("refs/stackit/metadata/")
	if err != nil {
		return nil, err
	}

	// Create a map of local branches for faster lookup
	localBranches := make(map[string]bool)
	for _, b := range e.state.branches {
		localBranches[b] = true
	}

	branchNames := make([]string, 0, len(localRefs))
	for refName := range localRefs {
		branchNames = append(branchNames, refName[len("refs/stackit/metadata/"):])
	}
	// Sorted so sync prompts in a stable order rather than map order.
	slices.Sort(branchNames)
	metas, metaErrs := e.batchReadMetadata(branchNames)

	orphaned := make([]OrphanedMetadataInfo, 0, len(localRefs))

	for _, branchName := range branchNames {
		// Metadata is orphaned if the local branch is gone
		existsLocally := localBranches[branchName]
		_, hasRemote := e.remoteMetaCache[branchName]

		// If it exists locally and has remote metadata, it's not orphaned (it's active and synced)
		if existsLocally && hasRemote {
			continue
		}

		// If it exists locally but has no remote metadata, it's not orphaned (it's a local-only branch)
		// UNLESS it was previously synced (has LocalOnlyHash).
		local := metas[branchName]
		if metaErrs[branchName] != nil || local == nil {
			continue
		}

		if existsLocally && local.GetLocalOnlyHash() == nil {
			continue
		}

		// At this point, metadata is orphaned if:
		// 1. The local branch is gone (manual deletion)
		// 2. The remote metadata is gone but was previously synced (dual-checkout scenario)

		// Check for local changes relative to last sync
		hasLocalChanges := false
		if local.GetLocalOnlyHash() != nil {
			hasLocalChanges = e.computeMetadataHash(local) != *local.GetLocalOnlyHash()
		}

		action := OrphanedActionDelete
		if hasLocalChanges && existsLocally {
			// Only prompt if the branch still exists locally but remote metadata is gone.
			// If the local branch is gone, we should just delete the metadata ref.
			action = OrphanedActionPrompt
		}

		orphaned = append(orphaned, OrphanedMetadataInfo{
			BranchName:      branchName,
			Action:          action,
			HasLocalChanges: hasLocalChanges,
			ExistsLocally:   existsLocally,
			LocalMeta:       local,
		})
	}

	return orphaned, nil
}

// CleanOrphanedMetadata reconciles branches whose remote metadata was deleted in
// a single transaction: it deletes the metadata ref entirely for branches whose
// local branch is also gone (deleteRefs) and clears just the local-only hash for
// branches that still exist locally (clearLocalHash). This replaces one git ref
// write per orphaned branch with a single batched write.
func (e *engineImpl) CleanOrphanedMetadata(ctx context.Context, deleteRefs []string, clearLocalHash []string) error {
	if len(deleteRefs) == 0 && len(clearLocalHash) == 0 {
		return nil
	}

	return e.WithRetry(ctx, func() error {
		tx := e.BeginTx(fmt.Sprintf("clean orphaned metadata: %d deleted, %d cleared", len(deleteRefs), len(clearLocalHash)))
		metas, _ := tx.ReadMetadata(ctx, append(append([]string{}, deleteRefs...), clearLocalHash...)...).Split()
		for _, name := range deleteRefs {
			if err := tx.DeleteMeta(name); err != nil {
				tx.Rollback()
				return err
			}
		}
		for _, name := range clearLocalHash {
			meta := metas[name]
			if meta == nil {
				continue
			}
			if err := tx.UpdateMeta(name, meta.WithLocalOnlyHash(nil)); err != nil {
				tx.Rollback()
				return err
			}
		}
		return tx.Commit(ctx)
	})
}

// FetchRemoteMetadata fetches metadata refs into the remote-metadata
// namespace, so the cache loader sees the latest authored values.
func (e *engineImpl) FetchRemoteMetadata(ctx context.Context) error {
	return e.FetchRemote(ctx, RemoteFetchRequest{IncludeMetadata: true})
}

// ConfigureRemoteMetadataSync adds the metadata refspec to the resolved remote
// so subsequent git fetches pick up metadata changes automatically. It is a
// no-op when that remote has no URL (e.g. a remote-less repo), so such a repo is
// never polluted with a dangling fetch refspec. The remote is resolved via
// GetRemote (the current branch's tracking remote, falling back to origin), so
// this works for non-origin remotes too.
func (e *engineImpl) ConfigureRemoteMetadataSync(_ context.Context) error {
	// `git config --get remote.<name>.url` exits non-zero when the remote does
	// not exist; that absence is precisely the "no remote to configure" signal,
	// so we treat the error as a no-op rather than a failure.
	url, err := e.git.GetConfig(fmt.Sprintf("remote.%s.url", e.GetRemote()))
	if err != nil {
		return nil //nolint:nilerr // missing remote.<name>.url means there is no remote to sync
	}
	if strings.TrimSpace(url) == "" {
		return nil
	}
	return e.git.EnsureMetadataRefspecConfigured()
}
