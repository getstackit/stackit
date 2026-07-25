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

	// Get current parent revision.
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

// ReadBranchRemoteStatuses returns the local/remote relationship for the
// requested branches. It lists remote branch SHAs once and does not mutate
// engine state.
func (e *engineImpl) ReadBranchRemoteStatuses(ctx context.Context, branches Branches) BranchRemoteStatuses {
	if ctx == nil {
		ctx = context.Background()
	}

	results := make(map[string]BranchRemoteStatus, len(branches))
	if len(branches) == 0 {
		return results
	}

	branchNames := branches.Names()
	localShas, _ := e.git.BatchGetRevisions(branchNames)

	remote := e.git.GetRemote()
	remoteShas, err := e.git.FetchRemoteShas(ctx, remote)
	if err != nil {
		remoteShas = map[string]string{}
	}

	for _, branch := range branches {
		branchName := branch.GetName()
		localSha := localShas[branchName]
		remoteSha := remoteShas[branchName]
		if remoteSha == "" {
			if sha, err := e.git.GetRemoteRevision(branchName); err == nil {
				remoteSha = sha
			}
		}

		status := BranchRemoteStatus{
			LocalSha:  localSha,
			RemoteSha: remoteSha,
		}

		switch {
		case localSha == "" || remoteSha == "":
		case localSha == remoteSha:
			status.CommonAncestor = localSha
		default:
			if commonAncestor, err := e.git.GetMergeBaseByRef(ctx, localSha, remoteSha); err == nil {
				status.CommonAncestor = commonAncestor
			}
		}

		results[branchName] = status
	}

	return results
}

// TrunkRemoteState reports how the local trunk relates to its remote-tracking
// branch using only local refs (a rev-parse of refs/remotes/<remote>/<trunk>
// plus an ancestry check). It never lists or fetches from the remote, so it is
// cheap enough for the restack path and works offline.
//
// When no remote-tracking trunk ref exists locally (local-only repo, never
// fetched, fresh clone) it returns HasRemoteRef=false and AheadOrDiverged=false
// so callers leave such repos unguarded.
func (e *engineImpl) TrunkRemoteState(ctx context.Context) TrunkRemoteState {
	if ctx == nil {
		ctx = context.Background()
	}
	e.mu.RLock()
	trunk := e.trunk
	e.mu.RUnlock()

	remote := e.git.GetRemote()
	state := TrunkRemoteState{RemoteRef: remote + "/" + trunk}

	remoteSha, err := e.git.GetRemoteSha(remote, trunk)
	if err != nil || remoteSha == "" {
		return state
	}
	state.HasRemoteRef = true
	state.RemoteSha = remoteSha

	localSha, err := e.git.GetRevision(trunk)
	if err != nil || localSha == "" {
		return state
	}
	state.LocalSha = localSha

	// Local trunk is "ahead or diverged" when it is NOT an ancestor of the
	// remote-tracking trunk. Equal (a commit is its own ancestor) and behind
	// both report ancestor=true, so neither trips the guard.
	if isAncestor, err := e.git.IsAncestor(ctx, localSha, remoteSha); err == nil && !isAncestor {
		state.AheadOrDiverged = true
	}
	return state
}

// GetMergedBranches returns the set of branches merged into the target branch
func (e *engineImpl) GetMergedBranches(ctx context.Context, target string) (BranchNameSet, error) {
	merged, err := e.git.GetMergedBranches(ctx, target)
	return BranchNameSet(merged), err
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

// BatchIsBranchEmpty reports, for each branch, whether it has no changes against
// its parent. A branch is empty iff its tree matches its parent's tree (trees
// are content-addressed, so equal SHA ⇔ no diff). All branch and parent tree
// SHAs are resolved in a single batched rev-parse rather than a `git diff` per
// branch. Branches whose trees cannot be resolved are omitted from the result.
func (e *engineImpl) BatchIsBranchEmpty(branchNames []string) BranchNameSet {
	result := make(BranchNameSet, len(branchNames))
	if len(branchNames) == 0 {
		return result
	}

	e.mu.RLock()
	trunk := e.trunk
	parents := make(map[string]string, len(branchNames))
	for _, name := range branchNames {
		parent := trunk
		if state := e.readState(name); state != nil {
			parent = state.Parent
		}
		parents[name] = parent
	}
	e.mu.RUnlock()

	// Collect each distinct ref's tree spec for one batched rev-parse.
	treeRefs := make([]string, 0, len(branchNames)*2)
	seen := make(map[string]struct{}, len(branchNames)*2)
	addRef := func(ref string) {
		spec := ref + "^{tree}"
		if _, ok := seen[spec]; ok {
			return
		}
		seen[spec] = struct{}{}
		treeRefs = append(treeRefs, spec)
	}
	for _, name := range branchNames {
		addRef(name)
		addRef(parents[name])
	}

	trees, _ := e.git.BatchGetRevisions(treeRefs)

	for _, name := range branchNames {
		branchTree, ok1 := trees[name+"^{tree}"]
		parentTree, ok2 := trees[parents[name]+"^{tree}"]
		if !ok1 || !ok2 {
			continue
		}
		result[name] = branchTree == parentTree
	}

	return result
}

// GetDeletionStatuses checks deletion status for multiple branches in a single batch.
// It batch-fetches metadata, revisions, and merged status, then evaluates the canonical
// deletion policy for each branch.
func (e *engineImpl) GetDeletionStatuses(ctx context.Context, branchNames []string) (DeletionStatuses, error) {
	results := make(DeletionStatuses, len(branchNames))
	if len(branchNames) == 0 {
		return results, nil
	}

	inputs, err := e.loadDeletionStatusInputs(ctx, branchNames)
	if err != nil {
		return nil, err
	}

	return e.evaluateDeletionStatuses(ctx, branchNames, inputs), nil
}

type deletionStatusInputs struct {
	trunkName      string
	metadataMap    MetaMap
	revisions      RevisionMap
	mergedBranches BranchNameSet
	squashCache    *git.SquashMergeCache
}

func (e *engineImpl) loadDeletionStatusInputs(ctx context.Context, branchNames []string) (*deletionStatusInputs, error) {
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

	return &deletionStatusInputs{
		trunkName:      trunkName,
		metadataMap:    metadataMap,
		revisions:      revisions,
		mergedBranches: mergedBranches,
		squashCache:    git.NewSquashMergeCache(),
	}, nil
}

func (e *engineImpl) evaluateDeletionStatuses(ctx context.Context, branchNames []string, inputs *deletionStatusInputs) DeletionStatuses {
	results := make(DeletionStatuses, len(branchNames))

	// Evaluate each branch
	for _, name := range branchNames {
		branch := e.GetBranch(name)
		meta := inputs.metadataMap[name]
		results[name] = e.evaluateDeletionStatus(ctx, name, branch, meta, inputs.revisions, inputs.mergedBranches, inputs.trunkName, inputs.squashCache)
	}

	return results
}

// evaluateDeletionStatus applies the canonical deletion policy for a single branch.
// Policy order:
//  1. Trunk guard → not deletable
//  2. Worktree anchor guard → not deletable
//  3. PR state CLOSED/MERGED → deletable
//  4. Merged into trunk → deletable
//  5. Empty with PR → deletable
func (e *engineImpl) evaluateDeletionStatus(ctx context.Context, branchName string, branch Branch, meta *git.Meta, revisions RevisionMap, mergedBranches BranchNameSet, trunkName string, squashCache *git.SquashMergeCache) DeletionStatus {
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
			if prInfo.State() == git.PRStateClosed {
				return DeletionStatus{SafeToDelete: true, Reason: "closed on GitHub", Kind: DeletionReasonClosedPR}
			}
			if prInfo.State() == git.PRStateMerged {
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

	// 4. Check if merged into trunk (ancestry-based; covers merge commits)
	if mergedBranches.Contains(branchName) {
		return DeletionStatus{
			SafeToDelete: true,
			Reason:       fmt.Sprintf("merged into %s", trunkName),
			Kind:         DeletionReasonMergedIntoTrunk,
		}
	}

	// 4b. Squash and rebase merges don't appear in the ancestry-based
	// mergedBranches set, and GitHub may have merged the PR before its local
	// state synced to MERGED (so rule 3 didn't fire either). For branches that
	// carry a recorded PR number, fall back to the same landed-branch detection
	// restack uses (cheap cherry plus the gated squash patch-id scan), so cleanup
	// keeps pace with squash-aware reparenting instead of leaving the merged
	// branch behind.
	//
	// Gated on a recorded PR number for two reasons: it never auto-deletes an
	// unsubmitted local branch whose diff happens to match trunk, and it keeps
	// the expensive patch-id scan off the common no-PR path (consistent with how
	// rule 5 gates its tree comparison behind PR metadata). branchLanded targets
	// trunk specifically, so a branch squash-merged into a still-open parent is
	// not treated as deletable.
	if metaHasSubmittedPR(meta) && e.branchLanded(ctx, branchName, trunkName, squashCache) {
		return DeletionStatus{
			SafeToDelete: true,
			Reason:       fmt.Sprintf("merged into %s", trunkName),
			Kind:         DeletionReasonMergedIntoTrunk,
		}
	}

	// 5. Check if empty (no diff from parent) with an associated PR.
	//
	// Rule 5 only fires when the branch is empty AND has a PR. Establishing
	// the PR side first lets us skip the IsDiffEmpty tree comparison entirely
	// for the (common) case of branches without PR metadata — that comparison
	// dominated branch-deletion planning when scaled to 20+ candidate branches.
	if meta == nil {
		return DeletionStatus{SafeToDelete: false, Reason: "", Kind: DeletionReasonNone}
	}
	if !metaHasSubmittedPR(meta) {
		return DeletionStatus{SafeToDelete: false, Reason: "", Kind: DeletionReasonNone}
	}

	parent := branch.GetParent()
	parentName := trunkName
	if parent != nil {
		parentName = parent.GetName()
	}

	if parentRev, ok := e.planRev(revisions, parentName); ok && parentRev != "" {
		if empty, err := e.git.IsDiffEmpty(ctx, branchName, parentRev); err == nil && empty {
			return DeletionStatus{SafeToDelete: true, Reason: "empty", Kind: DeletionReasonEmptyWithPR}
		}
	}

	return DeletionStatus{SafeToDelete: false, Reason: "", Kind: DeletionReasonNone}
}

// GetRemote returns the default remote name
func (e *engineImpl) GetRemote() string {
	return e.git.GetRemote()
}
