package engine

import (
	"context"
	"fmt"
)

// IsTrunk checks if a branch is the trunk
func (e *engineImpl) IsTrunk(branch Branch) bool {
	branchName := branch.GetName()
	e.mu.RLock()
	defer e.mu.RUnlock()
	return branchName == e.trunk
}

// IsTracked checks if a branch is tracked (has metadata)
func (e *engineImpl) IsTracked(branch Branch) bool {
	branchName := branch.GetName()
	e.mu.RLock()
	defer e.mu.RUnlock()
	_, ok := e.parentMap[branchName]
	return ok
}

// GetScope returns the scope for a branch, inheriting from parent if not set
func (e *engineImpl) GetScope(branch Branch) Scope {
	branchName := branch.GetName()
	e.mu.RLock()
	defer e.mu.RUnlock()

	current := branchName
	for {
		if scopeStr, ok := e.scopeMap[current]; ok && scopeStr != "" {
			scope := NewScope(scopeStr)
			if scope.IsNone() {
				return Empty()
			}
			return scope
		}
		parent, ok := e.parentMap[current]
		if !ok || parent == e.trunk {
			break
		}
		current = parent
	}
	return Empty()
}

// IsLocked checks if a branch is locked
func (e *engineImpl) IsLocked(branch Branch) bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return LockReason(e.lockedMap[branch.GetName()]).IsLocked()
}

// GetLockReason returns the reason why a branch is locked
func (e *engineImpl) GetLockReason(branch Branch) LockReason {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return LockReason(e.lockedMap[branch.GetName()])
}

// IsFrozen checks if a branch is frozen
func (e *engineImpl) IsFrozen(branch Branch) bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.frozenMap[branch.GetName()]
}

// GetExplicitScope returns the explicit scope set for a branch (no inheritance)
func (e *engineImpl) GetExplicitScope(branch Branch) Scope {
	branchName := branch.GetName()
	e.mu.RLock()
	defer e.mu.RUnlock()

	scopeStr := e.scopeMap[branchName]
	if scopeStr == "" {
		return Empty()
	}
	return NewScope(scopeStr)
}

// getExplicitScope is an internal method for Branch type
func (e *engineImpl) getExplicitScope(branch Branch) Scope {
	return e.GetExplicitScope(branch)
}

// IsUpToDate checks if a branch is up to date with its parent
// A branch is up to date if its parent revision matches the stored parent revision
func (e *engineImpl) IsUpToDate(branch Branch) bool {
	branchName := branch.GetName()
	if e.IsTrunk(branch) {
		return true
	}

	e.mu.RLock()
	parent, ok := e.parentMap[branchName]
	e.mu.RUnlock()

	if !ok {
		return true // Not tracked, consider it fixed
	}

	// Get current parent revision
	parentRev, err := e.git.GetRevision(parent)
	if err != nil {
		return false // Can't determine, assume needs restack
	}

	// Get stored parent revision from metadata
	meta, err := e.git.ReadMetadata(branchName)
	if err != nil {
		return false // No metadata, assume needs restack
	}

	if meta.ParentBranchRevision == nil {
		return false // No stored revision, needs restack
	}

	// Branch is fixed if stored revision matches current parent revision
	return *meta.ParentBranchRevision == parentRev
}

// GetBranchRemoteStatus returns the relationship between a local branch and its remote
func (e *engineImpl) GetBranchRemoteStatus(branchName string) (BranchRemoteStatus, error) {
	e.mu.RLock()
	remoteSha, hasCachedRemote := e.remoteShas[branchName]
	e.mu.RUnlock()

	localSha, err := e.git.GetRevision(branchName)
	if err != nil {
		localSha = "" // Branch doesn't exist locally
	}

	if !hasCachedRemote {
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
	parent, ok := e.parentMap[branchName]
	trunk := e.trunk
	e.mu.RUnlock()

	if !ok {
		// If not tracked, compare to trunk
		parent = trunk
	}

	// Get parent revision
	parentRev, err := e.git.GetRevision(parent)
	if err != nil {
		return false, err
	}

	return e.git.IsDiffEmpty(ctx, branchName, parentRev)
}

// GetDeletionStatus checks if a branch should be deleted
func (e *engineImpl) GetDeletionStatus(ctx context.Context, branchName string) (DeletionStatus, error) {
	// Check PR info
	branch := e.GetBranch(branchName)
	prInfo, err := e.GetPrInfo(branch)
	if err == nil && prInfo != nil {
		const (
			prStateClosed = "CLOSED"
			prStateMerged = "MERGED"
		)
		if prInfo.State() == prStateClosed {
			return DeletionStatus{SafeToDelete: true, Reason: fmt.Sprintf("%s is closed on GitHub", branchName)}, nil
		}
		if prInfo.State() == prStateMerged {
			base := prInfo.Base()
			if base == "" {
				base = e.Trunk().GetName()
			}
			return DeletionStatus{SafeToDelete: true, Reason: fmt.Sprintf("%s is merged into %s", branchName, base)}, nil
		}
	}

	// Check if merged into trunk
	merged, err := e.IsMergedIntoTrunk(ctx, branchName)
	if err == nil && merged {
		return DeletionStatus{SafeToDelete: true, Reason: fmt.Sprintf("%s is merged into %s", branchName, e.Trunk().GetName())}, nil
	}

	// Check if empty
	empty, err := e.IsBranchEmpty(ctx, branchName)
	if err == nil && empty {
		// Only delete empty branches if they have a PR
		if prInfo != nil && prInfo.Number() != nil {
			return DeletionStatus{SafeToDelete: true, Reason: fmt.Sprintf("%s is empty", branchName)}, nil
		}
	}

	return DeletionStatus{SafeToDelete: false, Reason: ""}, nil
}

// GetRemote returns the default remote name
func (e *engineImpl) GetRemote() string {
	return e.git.GetRemote()
}

// GetBranchRemoteDifference returns a string describing the difference between local and remote branch
func (e *engineImpl) GetBranchRemoteDifference(branchName string) (string, error) {
	status, err := e.GetBranchRemoteStatus(branchName)
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
