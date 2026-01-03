package merge

import (
	"context"
	"fmt"
	"strings"
	"time"

	"stackit.dev/stackit/internal/engine"
	"stackit.dev/stackit/internal/github"
	"stackit.dev/stackit/internal/tui"
)

// calculateBaselineEstimate tries to find a branch with successful CI and use its timing as a baseline
func calculateBaselineEstimate(ctx context.Context, plan *Plan, client github.Client, splog *tui.Splog) time.Duration {
	branchNames := make([]string, len(plan.BranchesToMerge))
	for i, b := range plan.BranchesToMerge {
		branchNames[i] = b.BranchName
	}

	statuses, err := client.BatchGetPRChecksStatus(ctx, branchNames)
	if err != nil {
		return 0
	}

	for _, branchInfo := range plan.BranchesToMerge {
		status := statuses[branchInfo.BranchName]
		if status == nil || !status.Passing || status.Pending {
			continue
		}

		// Found a passing PR, calculate the max duration of its checks
		var maxDuration time.Duration
		for _, check := range status.Checks {
			if !check.FinishedAt.IsZero() && !check.StartedAt.IsZero() {
				duration := check.FinishedAt.Sub(check.StartedAt)
				if duration > maxDuration {
					maxDuration = duration
				}
			}
		}

		if maxDuration > 0 {
			splog.Debug("Using PR #%d (%s) as timing baseline: %v", branchInfo.PRNumber, branchInfo.BranchName, maxDuration.Round(time.Second))
			return maxDuration
		}
	}
	return 0
}

func isConflictError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "hit conflict") ||
		strings.Contains(msg, "rebase conflict")
}

func isCIFailure(err error) bool {
	if err == nil {
		return false
	}
	errStr := fmt.Sprintf("%v", err)
	return strings.Contains(errStr, "CI checks failed") || strings.Contains(errStr, "failing CI checks") || strings.Contains(errStr, "pending CI checks")
}

// CheckSyncStatus checks if the repository is up to date with remote
func CheckSyncStatus(ctx context.Context, eng engine.Engine, splog *tui.Splog) (bool, []string, error) {
	needsSync := false
	staleBranches := []string{}

	// Check if trunk needs pulling
	pullResult, err := eng.PullTrunk(ctx)
	if err != nil {
		return false, nil, fmt.Errorf("failed to check trunk status: %w", err)
	}

	if pullResult == engine.PullDone {
		needsSync = true
		staleBranches = append(staleBranches, eng.Trunk().GetName())
	}

	// Check all tracked branches
	allBranches := eng.AllBranches()
	for _, branch := range allBranches {
		branchName := branch.GetName()
		branch := eng.GetBranch(branchName)
		if branch.IsTrunk() {
			continue
		}

		status, err := eng.GetBranchRemoteStatus(branchName)
		if err != nil {
			splog.Debug("Failed to get status for %s: %v", branchName, err)
			continue
		}

		if !status.Matches() {
			needsSync = true
			staleBranches = append(staleBranches, branchName)
		}
	}

	return needsSync, staleBranches, nil
}
