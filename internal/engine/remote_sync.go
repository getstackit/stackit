package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"stackit.dev/stackit/internal/git"
)

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

// SetLastModifiedBy updates the metadata for a branch with the current user's information
func (e *engineImpl) SetLastModifiedBy(branchName string) error {
	name, err := e.git.GetConfig("user.name")
	if err != nil {
		return fmt.Errorf("git user.name is required but not set: %w", err)
	}
	email, _ := e.git.GetConfig("user.email")

	modifiedBy := &git.ModifiedBy{
		GitName:  name,
		GitEmail: email,
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	meta, err := e.git.ReadMetadata(branchName)
	if err != nil {
		meta = &git.Meta{}
	}
	meta.LastModifiedBy = modifiedBy
	now := time.Now()
	meta.LastModifiedAt = &now

	// Update localOnlyHash for change detection
	hash := e.computeMetadataHash(meta)
	meta.LocalOnlyHash = &hash

	return e.git.WriteMetadata(branchName, meta)
}

// LoadRemoteMetadataCache loads remote metadata refs into the engine's cache
func (e *engineImpl) LoadRemoteMetadataCache() error {
	remoteRefs, err := e.git.ListRefs("refs/stackit/remote-metadata/")
	if err != nil {
		return err
	}

	cache := make(map[string]*git.Meta)
	for refName, sha := range remoteRefs {
		branchName := refName[len("refs/stackit/remote-metadata/"):]
		content, err := e.git.ReadBlob(sha)
		if err != nil {
			continue
		}

		var meta git.Meta
		if err := json.Unmarshal([]byte(content), &meta); err != nil {
			continue
		}
		cache[branchName] = &meta
	}

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

	// Update local metadata maps
	if remote.LockReason != git.LockReasonNone {
		e.lockedMap[branchName] = string(remote.LockReason)
	} else {
		delete(e.lockedMap, branchName)
	}
	if remote.Scope != nil {
		e.scopeMap[branchName] = *remote.Scope
	}

	// Read existing local metadata to preserve fields not in remote (like PrInfo)
	local, err := e.git.ReadMetadata(branchName)
	if err != nil {
		local = &git.Meta{}
	}

	// Update local with remote values
	local.LockReason = remote.LockReason
	local.Scope = remote.Scope
	local.BranchType = remote.BranchType
	local.LastModifiedBy = remote.LastModifiedBy
	local.LastModifiedAt = remote.LastModifiedAt

	// Store localOnlyHash for change detection
	hash := e.computeMetadataHash(local)
	local.LocalOnlyHash = &hash

	return e.git.WriteMetadata(branchName, local)
}

// GetRemoteMetadataCache returns the current remote metadata cache
func (e *engineImpl) GetRemoteMetadataCache() map[string]*git.Meta {
	e.mu.RLock()
	defer e.mu.RUnlock()

	// Return a copy to avoid external modification
	cacheCopy := make(map[string]*git.Meta)
	for k, v := range e.remoteMetaCache {
		cacheCopy[k] = v
	}
	return cacheCopy
}

// ComputeMetadataDiff compares local and remote metadata for a branch
func (e *engineImpl) ComputeMetadataDiff(branch string) (*MetadataDiff, error) {
	e.mu.RLock()
	local, err := e.git.ReadMetadata(branch)
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
	if local.LockReason != remote.LockReason {
		diff.Differences = append(diff.Differences, FieldDiff{
			Field:       "lockReason",
			LocalValue:  local.LockReason,
			RemoteValue: remote.LockReason,
		})
	}

	localScope := ""
	if local.Scope != nil {
		localScope = *local.Scope
	}
	remoteScope := ""
	if remote.Scope != nil {
		remoteScope = *remote.Scope
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
	for _, b := range e.branches {
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
	e.localModified[branch] = true
}

// HasLocalModifications checks if a branch has local metadata changes that differ from its original fetched state
func (e *engineImpl) HasLocalModifications(branch string) bool {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if e.localModified[branch] {
		return true
	}

	local, err := e.git.ReadMetadata(branch)
	if err != nil || local.LocalOnlyHash == nil {
		return false // Never synced or error, treat as not modified
	}

	currentHash := e.computeMetadataHash(local)
	return currentHash != *local.LocalOnlyHash
}

// computeMetadataHash calculates a hash of the syncable fields for change detection
func (e *engineImpl) computeMetadataHash(meta *git.Meta) string {
	// Hash of lockReason + scope (fields user can modify locally)
	scope := ""
	if meta.Scope != nil {
		scope = *meta.Scope
	}
	data := fmt.Sprintf("lockReason:%s,scope:%s", string(meta.LockReason), scope)
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
	LocalValue  interface{}
	RemoteValue interface{}
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
	for _, b := range e.branches {
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
		local, err := e.git.ReadMetadata(branchName)
		if err != nil {
			continue
		}

		if existsLocally && local.LocalOnlyHash == nil {
			continue
		}

		// At this point, metadata is orphaned if:
		// 1. The local branch is gone (manual deletion)
		// 2. The remote metadata is gone but was previously synced (dual-checkout scenario)

		// Check for local changes relative to last sync
		hasLocalChanges := false
		if local.LocalOnlyHash != nil {
			hasLocalChanges = e.computeMetadataHash(local) != *local.LocalOnlyHash
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

	local, err := e.git.ReadMetadata(branchName)
	if err != nil {
		return err
	}

	local.LocalOnlyHash = nil
	return e.git.WriteMetadata(branchName, local)
}

// DeleteMetadata deletes the metadata ref for a branch
func (e *engineImpl) DeleteMetadata(_ context.Context, branchName string) error {
	return e.git.DeleteMetadata(branchName)
}

// FetchRemoteMetadata fetches metadata refs from origin
func (e *engineImpl) FetchRemoteMetadata(ctx context.Context) error {
	_, err := e.git.RunGitCommandWithContext(ctx, "fetch", "origin", "+refs/stackit/metadata/*:refs/stackit/remote-metadata/*")
	return err
}

// ConfigureRemoteMetadataSync adds the metadata refspec to origin
func (e *engineImpl) ConfigureRemoteMetadataSync(ctx context.Context) error {
	_, err := e.git.RunGitCommandWithContext(ctx, "config", "--add", "remote.origin.fetch", "+refs/stackit/metadata/*:refs/stackit/remote-metadata/*")
	return err
}
