package actions

import (
	"context"
	"fmt"
	"strconv"

	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/errors"
)

// PROpenOptions configures how the target pull request is resolved.
type PROpenOptions struct {
	// BranchOrPR selects the pull request: a branch name, a PR number, or
	// empty to use the current branch.
	BranchOrPR string
	// Stack, when true, resolves to the root pull request of the stack (the
	// branch directly above trunk) instead of BranchOrPR's own pull request.
	Stack bool
}

// PRResult is the pull request resolved by PROpenAction.
type PRResult struct {
	Branch string
	Number *int
	URL    string
}

// PROpenAction resolves the pull request for a branch name, a PR number, or
// the current branch, so the caller can open it. It prefers local metadata
// (no network) and only calls GitHub when local metadata doesn't have the
// pull request yet, or when the argument is a PR number.
func PROpenAction(ctx *app.Context, opts PROpenOptions) (*PRResult, error) {
	eng := ctx.Engine
	remoteCtx, cancel := ctx.RemoteOperationContext()
	defer cancel()

	branchName := opts.BranchOrPR
	if branchName == "" {
		current := eng.CurrentBranch()
		if current == nil {
			return nil, errors.ErrNotOnBranch
		}
		branchName = current.GetName()
	} else if prNum, convErr := strconv.Atoi(branchName); convErr == nil {
		gh, err := ctx.RequireGitHub()
		if err != nil {
			return nil, fmt.Errorf("cannot resolve PR #%d: %w", prNum, err)
		}
		pr, err := gh.GetPullRequest(remoteCtx, prNum)
		if err != nil {
			return nil, fmt.Errorf("failed to get PR #%d: %w", prNum, err)
		}
		if !opts.Stack {
			return &PRResult{Branch: pr.Head, Number: &pr.Number, URL: pr.HTMLURL}, nil
		}
		branchName = pr.Head
	}

	if opts.Stack {
		branchName = shareStackBase(eng, branchName)
	}

	return resolvePRForBranch(ctx, remoteCtx, branchName)
}

// resolvePRForBranch resolves the pull request for a known branch name,
// preferring cached local metadata over a GitHub call.
func resolvePRForBranch(ctx *app.Context, remoteCtx context.Context, branchName string) (*PRResult, error) {
	branch := ctx.Engine.GetBranch(branchName)
	if prInfo, err := branch.GetPrInfo(); err == nil && prInfo != nil && prInfo.URL() != "" {
		return &PRResult{Branch: branchName, Number: prInfo.Number(), URL: prInfo.URL()}, nil
	}

	gh := ctx.GitHub()
	if gh == nil {
		return nil, fmt.Errorf("no pull request found for %q", branchName)
	}
	pr, err := gh.GetPullRequestByBranch(remoteCtx, branchName)
	if err != nil || pr == nil {
		return nil, fmt.Errorf("no pull request found for %q", branchName)
	}
	return &PRResult{Branch: branchName, Number: &pr.Number, URL: pr.HTMLURL}, nil
}
