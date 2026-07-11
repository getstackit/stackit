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

	return NewPrInfo(
		prInfo.Number,
		getStringValue(prInfo.Title),
		getStringValue(prInfo.Body),
		getStringValue(prInfo.State),
		getStringValue(prInfo.Base),
		getStringValue(prInfo.URL),
		getBoolValue(prInfo.IsDraft),
	).WithLockReason(LockReason(lockReason)).
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
		meta, err := e.readMetadata(branchName)
		if err != nil {
			meta = git.NewMeta()
		}
		meta = mergePrInfoIntoMeta(meta, prInfo)
		return e.withMetadataTx(ctx, fmt.Sprintf("upsert PR info: %s", branchName), func(tx *MetadataTx) error {
			return tx.UpdateMeta(branchName, meta)
		})
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
		metas, _ := e.batchReadMetadata(branchNames)

		tx := e.BeginTx(fmt.Sprintf("batch upsert PR info: %d branches", len(updates)))

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
// reading remote status once for the whole set instead of once per branch (a
// full `git ls-remote` each time). Results are keyed by branch name.
func (e *engineImpl) BatchGetPRSubmissionStatus(ctx context.Context, branches Branches) (map[string]PRSubmissionStatus, error) {
	// Only branches with an existing PR consult remote status; creates return
	// early without it. Read the remote a single time, and only if at least one
	// branch needs it, so an all-creates stack stays fully offline here.
	remoteStatuses := BranchRemoteStatuses{}
	for _, branch := range branches {
		prInfo, err := e.GetPrInfo(branch)
		if err == nil && prInfo != nil && prInfo.Number() != nil {
			remoteStatuses = e.ReadBranchRemoteStatuses(ctx, branches)
			break
		}
	}

	return e.BatchGetPRSubmissionStatusWithRemote(branches, remoteStatuses)
}

// BatchGetPRSubmissionStatusWithRemote is like BatchGetPRSubmissionStatus but
// uses a caller-supplied remote-status snapshot. This lets a caller read the
// remote once and reuse it — e.g. concurrently with other network work, or
// across planning and the later push — instead of reading it again here.
func (e *engineImpl) BatchGetPRSubmissionStatusWithRemote(branches Branches, remoteStatuses BranchRemoteStatuses) (map[string]PRSubmissionStatus, error) {
	results := make(map[string]PRSubmissionStatus, len(branches))
	for _, branch := range branches {
		status, err := e.prSubmissionStatus(branch, remoteStatuses.ForBranch(branch))
		if err != nil {
			return nil, err
		}
		results[branch.GetName()] = status
	}
	return results, nil
}

// prSubmissionStatus computes a branch's submission status from a precomputed
// remote status, so batched callers read the remote once.
func (e *engineImpl) prSubmissionStatus(branch Branch, remoteStatus BranchRemoteStatus) (PRSubmissionStatus, error) {
	prInfo, err := e.GetPrInfo(branch)
	if err != nil {
		return PRSubmissionStatus{}, err
	}

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
		}, nil
	}

	// It's an update
	baseChanged := prInfo.Base() != parentBranchName
	branchMatches := remoteStatus.Matches()

	// Check if PR title needs update due to scope changes
	titleNeedsUpdate := e.prTitleNeedsUpdate(branch, prInfo)

	// Check if lock status changed
	lockStatusChanged := false
	meta, err := e.readMetadata(branch.GetName())
	if err == nil {
		if meta.GetLockReason() != prInfo.LockReason() {
			lockStatusChanged = true
		} else {
			metaPrInfo := meta.GetPrInfo()
			if metaPrInfo != nil && metaPrInfo.LockReason != nil && *metaPrInfo.LockReason != prInfo.LockReason() {
				lockStatusChanged = true
			}
		}
	}

	// Check if merge branch changed
	mergeBranchChanged := false
	if err == nil {
		metaPrInfo := meta.GetPrInfo()
		if metaPrInfo != nil {
			oldBranch := getStringValue(metaPrInfo.MergeBranch)
			newBranch := prInfo.MergeBranch()
			if oldBranch != newBranch {
				mergeBranchChanged = true
			}
		}
	}

	needsUpdate := baseChanged || !branchMatches || titleNeedsUpdate || lockStatusChanged || mergeBranchChanged

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
	}, nil
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
