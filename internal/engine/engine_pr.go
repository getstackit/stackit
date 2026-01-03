package engine

import (
	"fmt"
	"regexp"
	"strings"

	"stackit.dev/stackit/internal/git"
)

// GetPrInfo returns PR information for a branch
func (e *engineImpl) GetPrInfo(branch Branch) (*PrInfo, error) {
	meta, err := e.git.ReadMetadata(branch.GetName())
	if err != nil {
		return nil, err
	}

	return NewPrInfoFromMeta(meta), nil
}

// NewPrInfoFromMeta creates a PrInfo from git.Meta
func NewPrInfoFromMeta(meta *git.Meta) *PrInfo {
	if meta == nil || meta.PrInfo == nil {
		return nil
	}

	lockReason := ""
	if meta.PrInfo.LockReason != nil {
		lockReason = string(*meta.PrInfo.LockReason)
	}

	return NewPrInfoFull(
		meta.PrInfo.Number,
		getStringValue(meta.PrInfo.Title),
		getStringValue(meta.PrInfo.Body),
		getStringValue(meta.PrInfo.State),
		getStringValue(meta.PrInfo.Base),
		getStringValue(meta.PrInfo.URL),
		getBoolValue(meta.PrInfo.IsDraft),
		LockReason(lockReason),
		getStringValue(meta.PrInfo.MergeBranch),
	)
}

// getPrInfo is an internal method for Branch type
func (e *engineImpl) getPrInfo(branch Branch) (*PrInfo, error) {
	return e.GetPrInfo(branch)
}

// UpsertPrInfo updates or creates PR information for a branch
func (e *engineImpl) UpsertPrInfo(branch Branch, prInfo *PrInfo) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	meta, err := e.git.ReadMetadata(branch.GetName())
	if err != nil {
		meta = &git.Meta{}
	}

	if prInfo == nil {
		meta.PrInfo = nil
		return e.git.WriteMetadata(branch.GetName(), meta)
	}

	if meta.PrInfo == nil {
		meta.PrInfo = &git.PrInfoPersistence{}
	}

	// Update PR info fields
	if prInfo.Number() != nil {
		meta.PrInfo.Number = prInfo.Number()
	}
	if prInfo.Title() != "" {
		title := prInfo.Title()
		meta.PrInfo.Title = &title
	}
	if prInfo.Body() != "" {
		body := prInfo.Body()
		meta.PrInfo.Body = &body
	}
	isDraft := prInfo.IsDraft()
	meta.PrInfo.IsDraft = &isDraft
	if prInfo.State() != "" {
		state := prInfo.State()
		meta.PrInfo.State = &state
	}
	if prInfo.Base() != "" {
		base := prInfo.Base()
		meta.PrInfo.Base = &base
	}
	if prInfo.URL() != "" {
		url := prInfo.URL()
		meta.PrInfo.URL = &url
	}
	lr := prInfo.LockReason()
	meta.PrInfo.LockReason = &lr
	mergeBranch := prInfo.MergeBranch()
	meta.PrInfo.MergeBranch = &mergeBranch

	return e.git.WriteMetadata(branch.GetName(), meta)
}

// GetPRSubmissionStatus returns the submission status of a branch
func (e *engineImpl) GetPRSubmissionStatus(branch Branch) (PRSubmissionStatus, error) {
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
			Action:      "create",
			NeedsUpdate: true,
			PRInfo:      prInfo,
		}, nil
	}

	// It's an update
	baseChanged := prInfo.Base() != parentBranchName
	status, _ := e.GetBranchRemoteStatus(branch)
	branchMatches := status.Matches()

	// Check if PR title needs update due to scope changes
	titleNeedsUpdate := e.prTitleNeedsUpdate(branch, prInfo)

	// Check if lock status changed
	lockStatusChanged := false
	meta, err := e.git.ReadMetadata(branch.GetName())
	if err == nil {
		if meta.LockReason != prInfo.LockReason() {
			lockStatusChanged = true
		} else if meta.PrInfo != nil && meta.PrInfo.LockReason != nil && *meta.PrInfo.LockReason != prInfo.LockReason() {
			lockStatusChanged = true
		}
	}

	// Check if merge branch changed
	mergeBranchChanged := false
	if err == nil && meta.PrInfo != nil {
		oldBranch := getStringValue(meta.PrInfo.MergeBranch)
		newBranch := prInfo.MergeBranch()
		if oldBranch != newBranch {
			mergeBranchChanged = true
		}
	}

	needsUpdate := baseChanged || !branchMatches || titleNeedsUpdate || lockStatusChanged || mergeBranchChanged

	reason := ""
	if !needsUpdate {
		reason = ReasonNoChanges
	}

	return PRSubmissionStatus{
		Action:      "update",
		NeedsUpdate: needsUpdate,
		Reason:      reason,
		PRNumber:    prInfo.Number(),
		PRInfo:      prInfo,
	}, nil
}

// getPRSubmissionStatus is an internal method for Branch type
func (e *engineImpl) getPRSubmissionStatus(branch Branch) (PRSubmissionStatus, error) {
	return e.GetPRSubmissionStatus(branch)
}

var scopeRegex = regexp.MustCompile(`^\[[^\]]+\]\s*`)

// prTitleNeedsUpdate checks if the PR title needs to be updated due to scope changes
func (e *engineImpl) prTitleNeedsUpdate(branch Branch, prInfo *PrInfo) bool {
	if prInfo == nil || prInfo.Title() == "" {
		return false
	}

	scope := e.GetScope(branch)
	updatedTitle := prInfo.Title()

	if !scope.IsEmpty() {
		// If title already has a scope prefix, replace it
		if scopeRegex.MatchString(updatedTitle) {
			// Only replace if it's NOT already the correct scope
			if !strings.HasPrefix(strings.ToUpper(updatedTitle), "["+strings.ToUpper(scope.String())+"]") {
				updatedTitle = scopeRegex.ReplaceAllString(updatedTitle, "["+scope.String()+"] ")
			}
		} else {
			// No scope prefix, add it
			updatedTitle = fmt.Sprintf("[%s] %s", scope.String(), updatedTitle)
		}
	}

	return updatedTitle != prInfo.Title()
}
