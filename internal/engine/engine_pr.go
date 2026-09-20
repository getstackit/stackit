package engine

import (
	"context"
	"fmt"
	"slices"

	"github.com/getstackit/stackit/internal/git"
)

// GetPrInfo returns PR information for a branch
func (e *engineImpl) GetPrInfo(branch Branch) (*PrInfo, error) {
	meta, err := e.readMetadata(branch.GetName())
	if err != nil {
		return nil, err
	}

	return NewPrInfoFromMeta(meta), nil
}

// NewPrInfoFromMeta creates a PrInfo from git.Meta
func NewPrInfoFromMeta(meta *git.Meta) *PrInfo {
	if meta == nil {
		return nil
	}
	prInfo := meta.GetPrInfo()
	if prInfo == nil {
		return nil
	}

	lockReason := ""
	if prInfo.LockReason != nil {
		lockReason = string(*prInfo.LockReason)
	}

	return NewPrInfo(PrInfoFields{
		Number:  prInfo.Number,
		Title:   getStringValue(prInfo.Title),
		Body:    getStringValue(prInfo.Body),
		State:   getStringValue(prInfo.State),
		Base:    getStringValue(prInfo.Base),
		URL:     getStringValue(prInfo.URL),
		IsDraft: getBoolValue(prInfo.IsDraft),
	}).WithLockReason(LockReason(lockReason)).
		WithMergeBranch(getStringValue(prInfo.MergeBranch)).
		WithBaseSHA(getStringValue(prInfo.BaseSHA))
}

// GetMergedDownstack returns the merged downstack history for a branch
func (e *engineImpl) GetMergedDownstack(branch Branch) []git.MergedParent {
	meta, err := e.readMetadata(branch.GetName())
	if err != nil {
		return nil
	}
	return meta.GetMergedDownstack()
}

// UpsertPrInfo updates or creates PR information for a branch with retry logic
// for concurrent modification resilience.
func (e *engineImpl) UpsertPrInfo(ctx context.Context, branch Branch, prInfo *PrInfo) error {
	branchName := branch.GetName()

	return e.WithRetry(ctx, func() error {
		tx := e.BeginTx(fmt.Sprintf("upsert PR info: %s", branchName))
		meta, err := tx.ReadMetadata(ctx, branchName).One()
		if err != nil {
			meta = git.NewMeta()
		}
		meta = mergePrInfoIntoMeta(meta, prInfo)
		if err := tx.UpdateMeta(branchName, meta); err != nil {
			return err
		}
		return tx.Commit(ctx)
	})
}

// BatchUpsertPrInfo updates PR information for multiple branches in one atomic
// transaction, replacing N serial git ref writes with a single batched write.
// The updates map is keyed by branch name; nil PrInfo clears the PR info.
func (e *engineImpl) BatchUpsertPrInfo(ctx context.Context, updates map[string]*PrInfo) error {
	if len(updates) == 0 {
		return nil
	}

	branchNames := make([]string, 0, len(updates))
	for name := range updates {
		branchNames = append(branchNames, name)
	}
	slices.Sort(branchNames)

	return e.WithRetry(ctx, func() error {
		tx := e.BeginTx(fmt.Sprintf("batch upsert PR info: %d branches", len(updates)))
		metas, _ := tx.ReadMetadata(ctx, branchNames...).Split()

		for _, name := range branchNames {
			meta, ok := metas[name]
			if !ok || meta == nil {
				meta = git.NewMeta()
			}
			meta = mergePrInfoIntoMeta(meta, updates[name])
			if err := tx.UpdateMeta(name, meta); err != nil {
				tx.Rollback()
				return err
			}
		}

		return tx.Commit(ctx)
	})
}

// mergePrInfoIntoMeta applies prInfo fields to meta's stored PR info and
// returns the updated meta. Non-zero fields in prInfo overwrite existing values.
func mergePrInfoIntoMeta(meta *git.Meta, prInfo *PrInfo) *git.Meta {
	if prInfo == nil {
		return meta.WithPrInfo(nil)
	}

	existing := meta.GetPrInfo()
	if existing == nil {
		existing = &git.PrInfoPersistence{}
	}

	if prInfo.Number() != nil {
		existing.Number = prInfo.Number()
	}
	if prInfo.Title() != "" {
		title := prInfo.Title()
		existing.Title = &title
	}
	if prInfo.Body() != "" {
		body := prInfo.Body()
		existing.Body = &body
	}
	isDraft := prInfo.IsDraft()
	existing.IsDraft = &isDraft
	if prInfo.State() != "" {
		state := prInfo.State()
		existing.State = &state
	}
	if prInfo.Base() != "" {
		base := prInfo.Base()
		existing.Base = &base
	}
	if prInfo.BaseSHA() != "" {
		baseSHA := prInfo.BaseSHA()
		existing.BaseSHA = &baseSHA
	}
	if prInfo.URL() != "" {
		url := prInfo.URL()
		existing.URL = &url
	}
	lr := prInfo.LockReason()
	existing.LockReason = &lr
	mergeBranch := prInfo.MergeBranch()
	existing.MergeBranch = &mergeBranch

	return meta.WithPrInfo(existing)
}

// BatchGetPRSubmissionStatus returns the submission status for every branch,
// keyed by branch name. remoteStatuses, when non-nil, is a caller-supplied
// remote-status snapshot — read once and reused, e.g. concurrently with other
// network work or across planning and the later push. When nil, the remote is
// read at most once for the whole set, and only if some branch has an existing
// PR (creates return early without it), so an all-creates stack stays fully
// offline. The context bounds that remote read.
func (e *engineImpl) BatchGetPRSubmissionStatus(ctx context.Context, branches Branches, remoteStatuses BranchRemoteStatuses) (map[string]PRSubmissionStatus, error) {
	metas, err := e.batchReadPrMetas(branches)
	if err != nil {
		return nil, err
	}

	if remoteStatuses == nil {
		remoteStatuses = BranchRemoteStatuses{}
		for _, branch := range branches {
			prInfo := NewPrInfoFromMeta(metas[branch.GetName()])
			if prInfo != nil && prInfo.Number() != nil {
				remoteStatuses = e.ReadBranchRemoteStatuses(ctx, branches)
				break
			}
		}
	}

	results := make(map[string]PRSubmissionStatus, len(branches))
	for _, branch := range branches {
		results[branch.GetName()] = e.prSubmissionStatus(branch, metas[branch.GetName()], remoteStatuses.ForBranch(branch))
	}
	return results, nil
}

// batchReadPrMetas resolves every branch's metadata in one batched read, so
// the per-branch status computation below never re-reads metadata it already
// has in hand. A failed read is an error, not a missing PR: treating it as
// "no PR" would plan a create for a branch that already has one.
func (e *engineImpl) batchReadPrMetas(branches Branches) (MetaMap, error) {
	branchNames := make([]string, len(branches))
	for i, b := range branches {
		branchNames[i] = b.GetName()
	}
	metas, errs := e.batchReadMetadata(branchNames)
	for _, name := range branchNames {
		if err := errs[name]; err != nil {
			return nil, fmt.Errorf("read metadata for %s: %w", name, err)
		}
	}
	return metas, nil
}

// prSubmissionStatus computes a branch's submission status from its
// precomputed metadata and remote status, so batched callers read metadata
// and the remote once each for the whole set rather than per branch.
func (e *engineImpl) prSubmissionStatus(branch Branch, meta *git.Meta, remoteStatus BranchRemoteStatus) PRSubmissionStatus {
	prInfo := NewPrInfoFromMeta(meta)

	parentBranch := e.GetParent(branch)
	parentBranchName := ""
	if parentBranch == nil {
		parentBranchName = e.trunk
	} else {
		parentBranchName = parentBranch.GetName()
	}

	if prInfo == nil || prInfo.Number() == nil {
		return PRSubmissionStatus{
			Action:      SubmitActionCreate,
			NeedsUpdate: true,
			PRInfo:      prInfo,
		}
	}

	// It's an update
	baseChanged := prInfo.Base() != parentBranchName
	branchMatches := remoteStatus.Matches()

	// Check if PR title needs update due to scope changes
	titleNeedsUpdate := e.prTitleNeedsUpdate(branch, prInfo)

	// The lock on the branch (set by `lock`) differs from the lock last
	// recorded with the PR, so the PR body's lock notice is stale. Merge-branch
	// changes need no check here: consolidation syncs the PRs itself.
	lockStatusChanged := meta.GetLockReason() != prInfo.LockReason()

	needsUpdate := baseChanged || !branchMatches || titleNeedsUpdate || lockStatusChanged

	reason := ""
	if !needsUpdate {
		reason = ReasonNoChanges
	}

	return PRSubmissionStatus{
		Action:      SubmitActionUpdate,
		NeedsUpdate: needsUpdate,
		Reason:      reason,
		PRNumber:    prInfo.Number(),
		PRInfo:      prInfo,
	}
}

// prTitleNeedsUpdate checks if the PR title needs to be updated due to scope changes
func (e *engineImpl) prTitleNeedsUpdate(branch Branch, prInfo *PrInfo) bool {
	if prInfo == nil || prInfo.Title() == "" {
		return false
	}

	scope := e.GetScope(branch)
	return scope.TitleNeedsUpdate(prInfo.Title())
}

// GetNavigationCommentID returns the cached navigation comment ID for a branch.
// Returns 0 if no comment ID is cached.
func (e *engineImpl) GetNavigationCommentID(branch Branch) (int64, error) {
	localMeta, err := e.readLocalMetadata(branch.GetName())
	if err != nil {
		return 0, err
	}
	if localMeta.NavigationCommentID == nil {
		return 0, nil
	}
	return *localMeta.NavigationCommentID, nil
}

// SetNavigationCommentID caches a navigation comment ID for a branch.
func (e *engineImpl) SetNavigationCommentID(branch Branch, commentID int64) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	localMeta, err := e.readLocalMetadata(branch.GetName())
	if err != nil {
		localMeta = &git.LocalMeta{}
	}

	localMeta.NavigationCommentID = &commentID
	return e.writeLocalMetadata(branch.GetName(), localMeta)
}

// ClearNavigationCommentID removes the cached navigation comment ID for a branch.
func (e *engineImpl) ClearNavigationCommentID(branch Branch) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	localMeta, err := e.readLocalMetadata(branch.GetName())
	if err != nil {
		return nil // Nothing to clear if we can't read
	}

	if localMeta.NavigationCommentID == nil {
		return nil // Already clear
	}

	localMeta.NavigationCommentID = nil
	return e.writeLocalMetadata(branch.GetName(), localMeta)
}
