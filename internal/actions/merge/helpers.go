package merge

import (
	"context"
	"strings"
	"time"

	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/internal/github"
	"github.com/getstackit/stackit/internal/output"
)

type pauseResumer interface {
	Pause()
	Resume()
}

func getMergeMethodWithPause(ctx context.Context, deps Deps, githubClient github.Client, handler EventHandler) (github.MergeMethod, error) {
	if handler != nil {
		if pr, ok := handler.(pauseResumer); ok {
			pr.Pause()
			defer pr.Resume()
		}
	}
	return GetMergeMethod(ctx, deps.Out, deps.Prompts, deps.Config, githubClient)
}

// calculateBaselineEstimate tries to find a branch with successful CI and use its timing as a baseline
func calculateBaselineEstimate(ctx context.Context, plan *Plan, client github.Client, splog output.Output) time.Duration {
	remoteCtx, cancelRemote := git.WithRemoteOperationTimeout(ctx)
	defer cancelRemote()
	statuses, err := client.BatchGetPRChecksStatus(remoteCtx, plan.BranchNames())
	if err != nil {
		return 0
	}

	for _, branchInfo := range plan.BranchesToMerge {
		status := statuses[branchInfo.BranchName]
		if status == nil || !status.Passing || status.Pending {
			continue
		}

		// Found a passing PR, calculate the max duration of its checks
		maxDuration := maxCheckDuration(status.Checks)

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
	msg := err.Error()
	return strings.Contains(msg, "CI checks failed") || strings.Contains(msg, "failing CI checks") || strings.Contains(msg, "pending CI checks")
}

// maxCheckDuration returns the longest run time among checks that have both a
// start and finish time, or zero when none do.
func maxCheckDuration(checks []github.CheckDetail) time.Duration {
	var longest time.Duration
	for _, check := range checks {
		if check.FinishedAt.IsZero() || check.StartedAt.IsZero() {
			continue
		}
		longest = max(longest, check.FinishedAt.Sub(check.StartedAt))
	}
	return longest
}
