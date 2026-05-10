package merge

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/github"
	"github.com/getstackit/stackit/internal/output"
)

type pauseResumer interface {
	Pause()
	Resume()
}

func getMergeMethodWithPause(ctx *app.Context, githubClient github.Client, handler EventHandler) (github.MergeMethod, error) {
	if handler != nil {
		if pr, ok := handler.(pauseResumer); ok {
			pr.Pause()
			defer pr.Resume()
		}
	}
	return GetMergeMethod(ctx, githubClient)
}

// calculateBaselineEstimate tries to find a branch with successful CI and use its timing as a baseline
func calculateBaselineEstimate(ctx context.Context, plan *Plan, client github.Client, splog output.Output) time.Duration {
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
