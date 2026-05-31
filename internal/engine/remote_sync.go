package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/internal/utils"
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

// SetRemoteSyncEnabled marks metadata compatibility as verified and enables/disables sync
func (e *engineImpl) SetRemoteSyncEnabled(enabled bool) {
	val := "false"
	if enabled {
		val = "true"
	}
	_ = e.git.SetConfig("stackit.metadata-sync-enabled", val)
}

// BatchSetLastModifiedBy updates metadata for multiple branches in parallel
// with a single git config lookup
func (e *engineImpl) BatchSetLastModifiedBy(branchNames []string) error {
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

	// Parallel metadata updates
	var firstErr error
	var errMu sync.Mutex

	utils.Run(branchNames, func(branchName string) {
		if err := e.setLastModifiedByInternal(branchName, modifiedBy); err != nil {
			errMu.Lock()
			if firstErr == nil {
				firstErr = err
			}
			errMu.Unlock()
		}
	})

	return firstErr
}

// setLastModifiedByInternal updates metadata for a single branch with pre-fetched user info
func (e *engineImpl) setLastModifiedByInternal(branchName string, modifiedBy *git.ModifiedBy) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	meta, err := e.readMetadata(branchName)
	if err != nil {
		meta = git.NewMeta()
	}
	now := time.Now()
	meta = meta.WithLastModifiedBy(modifiedBy).WithLastModifiedAt(&now)

	// Update localOnlyHash for change detection
	hash := e.computeMetadataHash(meta)
	meta = meta.WithLocalOnlyHash(&hash)

	return e.writeMetadata(branchName, meta)
}

// LoadRemoteMetadataCache loads remote metadata refs into the engine's cache
func (e *engineImpl) LoadRemoteMetadataCache() error {
	remoteRefs, err := e.git.ListRefs("refs/stackit/remote-metadata/")
	if err != nil {
		return err
	}

	// Collect refs into a slice for parallel processing
	type refInfo struct {
		branch string
		sha    string
	}
	refs := make([]refInfo, 0, len(remoteRefs))
	for refName, sha := range remoteRefs {
		branch := refName[len("refs/stackit/remote-metadata/"):]
		refs = append(refs, refInfo{branch, sha})
	}

	// Parallel blob reads
	cache := make(map[string]*git.Meta)
	var cacheMu sync.Mutex

	utils.Run(refs, func(ref refInfo) {
		content, err := e.git.ReadBlob(ref.sha)
		if err != nil {
			return
		}

		var meta git.Meta
		if err := json.Unmarshal([]byte(content), &meta); err != nil {
			return
		}

		cacheMu.Lock()
		cache[ref.branch] = &meta
		cacheMu.Unlock()
	})

	e.mu.Lock()
	defer e.mu.Unlock()
	e.remoteMetaCache = cache
	return nil
}

// ApplyRemoteMetadataIfExists applies remote metadata to a local branch if it exists in the cache
func (e *engineImpl) ApplyRemoteMetadataIfExists(branchName string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	remote, ok := e.remoteMetaCache[branchName]
	if !ok {
		return nil
	}

	// Update local branch state
	if state := e.readState(branchName); state != nil {
		state.LockReason = remote.GetLockReason()
		if remote.GetScope() != nil {
			state.Scope = *remote.GetScope()
		}
	}

	// Read existing local metadata to preserve fields not in remote (like PrInfo)
	local, err := e.readMetadata(branchName)
	if err != nil {
		local = git.NewMeta()
	}

	// Update local with remote values
	local = local.
		WithLockReason(remote.GetLockReason()).
		WithScope(remote.GetScope()).
		WithBranchType(remote.GetBranchType()).
		WithLastModifiedBy(remote.GetLastModifiedBy()).
		WithLastModifiedAt(remote.GetLastModifiedAt())

	// Store localOnlyHash for change detection
	hash := e.computeMetadataHash(local)
	local = local.WithLocalOnlyHash(&hash)

	return e.writeMetadata(branchName, local)
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

	if remote == nil {
		return nil, nil // No remote metadata, nothing to diff
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
	return diff, nil
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
	for b := range e.remoteMetaCache {
		if localBranches[b] {
			branches = append(branches, b)
		}
	}
	e.mu.RUnlock()

	var diffs []*MetadataDiff
	for _, branch := range branches {
		diff, err := e.ComputeMetadataDiff(branch)
		if err != nil {
			return nil, err
		}
		if diff != nil && diff.HasConflict {
			diffs = append(diffs, diff)
		}
	}
	return diffs, nil
}

// AcceptRemoteMetadata overwrites local metadata with remote values
func (e *engineImpl) AcceptRemoteMetadata(branch string) error {
	return e.ApplyRemoteMetadataIfExists(branch)
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

	orphaned := make([]OrphanedMetadataInfo, 0, len(localRefs))

	for refName := range localRefs {
		branchName := refName[len("refs/stackit/metadata/"):]

		// Metadata is orphaned if the local branch is gone
		existsLocally := localBranches[branchName]
		_, hasRemote := e.remoteMetaCache[branchName]

		// If it exists locally and has remote metadata, it's not orphaned (it's active and synced)
		if existsLocally && hasRemote {
			continue
		}

		// If it exists locally but has no remote metadata, it's not orphaned (it's a local-only branch)
		// UNLESS it was previously synced (has LocalOnlyHash).
		local, err := e.readMetadata(branchName)
		if err != nil {
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

// DeleteLocalMetadataHash removes the LocalOnlyHash from a branch's metadata
// This effectively "un-syncs" the branch so it won't be considered orphaned
func (e *engineImpl) DeleteLocalMetadataHash(branchName string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	local, err := e.readMetadata(branchName)
	if err != nil {
		return err
	}

	local = local.WithLocalOnlyHash(nil)
	return e.writeMetadata(branchName, local)
}

// DeleteMetadata deletes the metadata ref for a branch with retry logic.
// Uses the transaction API to ensure concurrent modification resilience
// and proper in-memory cache cleanup.
func (e *engineImpl) DeleteMetadata(ctx context.Context, branchName string) error {
	return e.WithRetry(ctx, func() error {
		tx := e.BeginTx(fmt.Sprintf("delete metadata: %s", branchName))
		if err := tx.DeleteMeta(branchName); err != nil {
			return err
		}
		return tx.Commit(ctx)
	})
}

// FetchRemoteMetadata fetches metadata refs into the remote-metadata
// namespace, so the cache loader sees the latest authored values.
func (e *engineImpl) FetchRemoteMetadata(ctx context.Context) error {
	return e.FetchRemote(ctx, RemoteFetchRequest{IncludeMetadata: true})
}

// ConfigureRemoteMetadataSync adds the metadata refspec to the configured
// remote so subsequent git fetches pick up metadata changes automatically.
func (e *engineImpl) ConfigureRemoteMetadataSync(_ context.Context) error {
	return e.git.EnsureMetadataRefspecConfigured()
}
