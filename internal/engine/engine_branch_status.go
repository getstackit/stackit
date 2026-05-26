package engine

import (
	"context"
	"fmt"
	"slices"

	"github.com/getstackit/stackit/internal/git"
)

// IsTrunk checks if a branch is the trunk
func (e *engineImpl) IsTrunk(branch Branch) bool {
	branchName := branch.GetName()
	e.mu.RLock()
	defer e.mu.RUnlock()
	return branchName == e.trunk
}

// IsTracked checks if a branch is tracked (has a parent in metadata)
func (e *engineImpl) IsTracked(branch Branch) bool {
	e.ensureBranchSharedLoaded(branch.GetName())
	e.mu.RLock()
	defer e.mu.RUnlock()
	state := e.readState(branch.GetName())
	return state != nil && state.Parent != ""
}

// GetScope returns the scope for a branch, inheriting from parent if not set
func (e *engineImpl) GetScope(branch Branch) Scope {
	current := branch.GetName()
	visited := make(map[string]bool)
	for !visited[current] {
		visited[current] = true

		e.ensureBranchSharedLoaded(current)
		e.mu.RLock()
		state := e.readState(current)
		if state == nil {
			e.mu.RUnlock()
			break
		}

		hasScope := state.HasScope()
		scope := state.GetScope()
		parent := state.Parent
		trunk := e.trunk
		e.mu.RUnlock()

		if hasScope {
			if scope.IsNone() {
				return Empty()
			}
			return scope
		}
		if parent == "" || parent == trunk {
			break
		}
		current = parent
	}
	return Empty()
}

// IsLocked checks if a branch is locked
func (e *engineImpl) IsLocked(branch Branch) bool {
	e.ensureBranchSharedLoaded(branch.GetName())
	e.mu.RLock()
	defer e.mu.RUnlock()
	if state := e.readState(branch.GetName()); state != nil {
		return state.IsLocked()
	}
	return false
}

// GetLockReason returns the reason why a branch is locked
func (e *engineImpl) GetLockReason(branch Branch) LockReason {
	e.ensureBranchSharedLoaded(branch.GetName())
	e.mu.RLock()
	defer e.mu.RUnlock()
	if state := e.readState(branch.GetName()); state != nil {
		return state.LockReason
	}
	return LockReasonNone
}

// IsFrozen checks if a branch is frozen.
// Frozen lives in local metadata which is loaded lazily — promote the local
// tier before reading. Under LoadModeFull this is a no-op atomic check.
func (e *engineImpl) IsFrozen(branch Branch) bool {
	e.ensureLocalLoaded()
	e.mu.RLock()
	defer e.mu.RUnlock()
	if state := e.readState(branch.GetName()); state != nil {
		return state.Frozen
	}
	return false
}

// GetBranchType returns the branch type for a branch
func (e *engineImpl) GetBranchType(branch Branch) git.BranchType {
	e.ensureBranchSharedLoaded(branch.GetName())
	e.mu.RLock()
	defer e.mu.RUnlock()
	if state := e.readState(branch.GetName()); state != nil {
		return state.BranchType
	}
	return ""
}

// IsWorktreeAnchor checks if a branch is a worktree anchor branch
func (e *engineImpl) IsWorktreeAnchor(branch Branch) bool {
	return e.GetBranchType(branch) == git.BranchTypeWorktreeAnchor
}

// SetBranchType sets the branch type for a branch
func (e *engineImpl) SetBranchType(branch Branch, branchType git.BranchType) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	branchName := branch.GetName()

	// Read existing metadata
	meta, err := e.readMetadata(branchName)
	if err != nil {
		return fmt.Errorf("failed to read metadata: %w", err)
	}

	// Update branch type
	meta = meta.WithBranchType(branchType)

	// Write metadata
	if err := e.writeMetadata(branchName, meta); err != nil {
		return fmt.Errorf("failed to write metadata: %w", err)
	}

	// Update in-memory state
	if state := e.readState(branchName); state != nil {
		state.BranchType = branchType
	}

	return nil
}

// GetExplicitScope returns the explicit scope set for a branch (no inheritance)
func (e *engineImpl) GetExplicitScope(branch Branch) Scope {
	e.ensureBranchSharedLoaded(branch.GetName())
	e.mu.RLock()
	defer e.mu.RUnlock()

	if state := e.readState(branch.GetName()); state != nil && state.HasScope() {
		return state.GetScope()
	}
	return Empty()
}

// getExplicitScope is an internal method for Branch type
func (e *engineImpl) getExplicitScope(branch Branch) Scope {
	return e.GetExplicitScope(branch)
}

// IsUpToDate checks if a branch is up to date with its parent
// A branch is up to date if its parent revision matches the stored parent revision
func (e *engineImpl) IsUpToDate(branch Branch) bool {
	if e.IsTrunk(branch) {
		return true
	}

	e.mu.RLock()
	state := e.readState(branch.GetName())
	e.mu.RUnlock()

	if state == nil {
		return true // Not tracked, consider it fixed
	}

	if state.ParentBranchRevision == "" {
		return false // No stored revision, needs restack
	}

	// Get current parent revision (cached via revisionCache after first read).
	parentRev, err := e.git.GetRevision(state.Parent)
	if err != nil {
		return false // Can't determine, assume needs restack
	}

	// Branch is up to date if stored revision matches current parent revision.
	return state.ParentBranchRevision == parentRev
}

// ReadBranchStatuses reads status facts for a group of branches.
// Parent revisions are fetched in one batch. Missing metadata follows
// IsUpToDate semantics.
func (e *engineImpl) ReadBranchStatuses(branches Branches) BranchStatuses {
	results := make(map[string]bool, branches.Len())
	type pendingCheck struct {
		parent      string
		expectedRev string
	}
	pending := make(map[string]pendingCheck, branches.Len())
	parentSet := make(map[string]struct{}, branches.Len())

	e.mu.RLock()
	trunk := e.trunk
	for _, branch := range branches {
		name := branch.GetName()
		if name == trunk {
			results[name] = true
			continue
		}

		state := e.readState(name)
		if state == nil {
			results[name] = true
			continue
		}

		if state.ParentBranchRevision == "" {
			results[name] = false
			continue
		}

		pending[name] = pendingCheck{
			parent:      state.Parent,
			expectedRev: state.ParentBranchRevision,
		}
		parentSet[state.Parent] = struct{}{}
	}
	e.mu.RUnlock()

	if len(parentSet) == 0 {
		return newBranchStatuses(results)
	}

	parents := make([]string, 0, len(parentSet))
	for parent := range parentSet {
		parents = append(parents, parent)
	}
	slices.Sort(parents)

	parentRevs, _ := e.git.BatchGetRevisions(parents)
	for name, check := range pending {
		parentRev, ok := parentRevs[check.parent]
		results[name] = ok && check.expectedRev == parentRev
	}

	return newBranchStatuses(results)
}

// GetBranchRemoteStatus returns the relationship between a local branch and its remote
func (e *engineImpl) GetBranchRemoteStatus(branch Branch) (BranchRemoteStatus, error) {
	branchName := branch.GetName()
	e.mu.RLock()
	state := e.readState(branch.GetName())
	var remoteSha string
	if state != nil {
		remoteSha = state.RemoteSHA
	}
	e.mu.RUnlock()

	localSha, err := e.git.GetRevision(branchName)
	if err != nil {
		localSha = "" // Branch doesn't exist locally
	}

	if remoteSha == "" {
		// Fall back to local remote tracking branch
		remoteSha, err = e.git.GetRemoteRevision(branchName)
		if err != nil {
			remoteSha = "" // No remote tracking branch
		}
	}

	status := BranchRemoteStatus{
		LocalSha:  localSha,
		RemoteSha: remoteSha,
	}

	if localSha == "" || remoteSha == "" {
		return status, nil
	}

	if localSha == remoteSha {
		status.CommonAncestor = localSha
		return status, nil
	}

	// They differ, compute common ancestor to determine relation
	remote := e.git.GetRemote()
	remoteBranchRef := "refs/remotes/" + remote + "/" + branchName
	commonAncestor, err := e.git.GetMergeBaseByRef(branchName, remoteBranchRef)
	if err == nil {
		status.CommonAncestor = commonAncestor
	}

	return status, nil
}

// GetMergedBranches returns a map of branches merged into the target branch
func (e *engineImpl) GetMergedBranches(ctx context.Context, target string) (map[string]bool, error) {
	return e.git.GetMergedBranches(ctx, target)
}

// IsMergedIntoTrunk checks if a branch is merged into trunk
func (e *engineImpl) IsMergedIntoTrunk(ctx context.Context, branchName string) (bool, error) {
	e.mu.RLock()
	trunk := e.trunk
	e.mu.RUnlock()
	return e.git.IsMerged(ctx, branchName, trunk)
}

// IsBranchEmpty checks if a branch has no changes compared to its parent
func (e *engineImpl) IsBranchEmpty(ctx context.Context, branchName string) (bool, error) {
	e.mu.RLock()
	state := e.readState(branchName)
	trunk := e.trunk
	e.mu.RUnlock()

	parent := trunk
	if state != nil {
		parent = state.Parent
	}

	// Get parent revision
	parentRev, err := e.git.GetRevision(parent)
	if err != nil {
		return false, err
	}

	return e.git.IsDiffEmpty(ctx, branchName, parentRev)
}

// GetDeletionStatus checks if a branch should be deleted.
// Thin wrapper around BatchGetDeletionStatuses for a single branch.
func (e *engineImpl) GetDeletionStatus(ctx context.Context, branchName string) (DeletionStatus, error) {
	statuses, err := e.BatchGetDeletionStatuses(ctx, []string{branchName})
	if err != nil {
		return DeletionStatus{}, err
	}
	if status, ok := statuses[branchName]; ok {
		return status, nil
	}
	return DeletionStatus{SafeToDelete: false, Reason: "", Kind: DeletionReasonNone}, nil
}

// BatchGetDeletionStatuses checks deletion status for multiple branches in a single batch.
// It batch-fetches metadata, revisions, and merged status, then evaluates the canonical
// deletion policy for each branch.
func (e *engineImpl) BatchGetDeletionStatuses(ctx context.Context, branchNames []string) (map[string]DeletionStatus, error) {
	results := make(map[string]DeletionStatus, len(branchNames))
	if len(branchNames) == 0 {
		return results, nil
	}

	trunkName := e.Trunk().GetName()

	// Collect all refs we need revisions for (branches + their parents)
	refsToFetch := make([]string, 0, len(branchNames)*2+1)
	refsToFetch = append(refsToFetch, trunkName)
	for _, name := range branchNames {
		refsToFetch = append(refsToFetch, name)
		branch := e.GetBranch(name)
		parent := branch.GetParent()
		if parent != nil {
			refsToFetch = append(refsToFetch, parent.GetName())
		}
	}

	// Batch fetch all data
	metadataMap, _ := e.batchReadMetadata(branchNames)
	revisions, _ := e.git.BatchGetRevisions(refsToFetch)
	mergedBranches, err := e.GetMergedBranches(ctx, trunkName)
	if err != nil {
		return nil, fmt.Errorf("failed to check merged branches: %w", err)
	}

	// Evaluate each branch
	for _, name := range branchNames {
		branch := e.GetBranch(name)
		meta := metadataMap[name]
		results[name] = e.evaluateDeletionStatus(ctx, name, branch, meta, revisions, mergedBranches, trunkName)
	}

	return results, nil
}

// evaluateDeletionStatus applies the canonical deletion policy for a single branch.
// Policy order:
//  1. Trunk guard → not deletable
//  2. Worktree anchor guard → not deletable
//  3. PR state CLOSED/MERGED → deletable
//  4. Merged into trunk → deletable
//  5. Empty with PR → deletable
func (e *engineImpl) evaluateDeletionStatus(ctx context.Context, branchName string, branch Branch, meta *git.Meta, revisions map[string]string, mergedBranches map[string]bool, trunkName string) DeletionStatus {
	// 1. Never delete trunk
	if e.IsTrunk(branch) {
		return DeletionStatus{SafeToDelete: false, Reason: "", Kind: DeletionReasonNone}
	}

	// 2. Never delete worktree anchor branches
	if e.IsWorktreeAnchor(branch) {
		return DeletionStatus{SafeToDelete: false, Reason: "", Kind: DeletionReasonNone}
	}

	// 3. Check PR state from metadata
	if meta != nil {
		prInfo := NewPrInfoFromMeta(meta)
		if prInfo != nil {
			const (
				prStateClosed = "CLOSED"
				prStateMerged = "MERGED"
			)
			if prInfo.State() == prStateClosed {
				return DeletionStatus{SafeToDelete: true, Reason: "closed on GitHub", Kind: DeletionReasonClosedPR}
			}
			if prInfo.State() == prStateMerged {
				base := prInfo.Base()
				if base == "" {
					base = trunkName
				}
				return DeletionStatus{
					SafeToDelete: true,
					Reason:       fmt.Sprintf("merged into %s", base),
					Kind:         DeletionReasonMergedPR,
				}
			}
		}
	}

	// 4. Check if merged into trunk
	if mergedBranches != nil && mergedBranches[branchName] {
		return DeletionStatus{
			SafeToDelete: true,
			Reason:       fmt.Sprintf("merged into %s", trunkName),
			Kind:         DeletionReasonMergedIntoTrunk,
		}
	}

	// 5. Check if empty (no diff from parent) with an associated PR
	parent := branch.GetParent()
	parentName := trunkName
	if parent != nil {
		parentName = parent.GetName()
	}

	parentRev := revisions[parentName]
	if parentRev == "" {
		if rev, revErr := e.git.GetRevision(parentName); revErr == nil {
			parentRev = rev
		}
	}
	if parentRev != "" {
		empty, err := e.git.IsDiffEmpty(ctx, branchName, parentRev)
		if err == nil && empty {
			// Only delete empty branches if they have a PR
			if meta != nil {
				metaPrInfo := meta.GetPrInfo()
				if metaPrInfo != nil && metaPrInfo.Number != nil && *metaPrInfo.Number != 0 {
					return DeletionStatus{SafeToDelete: true, Reason: "empty", Kind: DeletionReasonEmptyWithPR}
				}
			}
		}
	}

	return DeletionStatus{SafeToDelete: false, Reason: "", Kind: DeletionReasonNone}
}

// GetRemote returns the default remote name
func (e *engineImpl) GetRemote() string {
	return e.git.GetRemote()
}

// GetBranchRemoteDifference returns a string describing the difference between local and remote branch
func (e *engineImpl) GetBranchRemoteDifference(branchName string) (string, error) {
	branch := e.GetBranch(branchName)
	status, err := e.GetBranchRemoteStatus(branch)
	if err != nil {
		return "", err
	}

	if status.LocalSha == "" {
		return "(branch not found locally)", nil
	}

	localShort := status.LocalSha
	if len(localShort) > 7 {
		localShort = localShort[:7]
	}

	if status.RemoteSha == "" {
		return fmt.Sprintf("local: %s (branch not found on remote)", localShort), nil
	}

	if status.Matches() {
		return "", nil
	}

	remoteShort := status.RemoteSha
	if len(remoteShort) > 7 {
		remoteShort = remoteShort[:7]
	}

	switch {
	case status.Behind():
		return fmt.Sprintf("local is behind remote (local: %s, remote: %s)", localShort, remoteShort), nil
	case status.Ahead():
		return fmt.Sprintf("local is ahead of remote (local: %s, remote: %s)", localShort, remoteShort), nil
	case status.Diverged():
		return fmt.Sprintf("local and remote have diverged (local: %s, remote: %s)", localShort, remoteShort), nil
	default:
		// If common ancestor couldn't be determined but they are different
		return fmt.Sprintf("local: %s, remote: %s", localShort, remoteShort), nil
	}
}
