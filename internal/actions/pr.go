package actions

import (
	"fmt"
	"strconv"

	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/utils"
)

// PROptions configures PRAction.
type PROptions struct {
	// Stack opens the root pull request of the stack (the branch directly
	// above trunk) instead of the resolved branch's own pull request.
	Stack bool
}

// PRAction resolves branchOrPR to a pull request URL and opens it in the
// default browser. branchOrPR may be a branch name, a PR number, or empty
// (defaults to the current branch).
func PRAction(ctx *app.Context, branchOrPR string, opts PROptions) error {
	eng := ctx.Engine

	targetBranch, prURL, err := resolvePRTarget(ctx, branchOrPR)
	if err != nil {
		return err
	}

	if opts.Stack {
		if rootBranch := shareStackBase(eng, targetBranch); rootBranch != targetBranch {
			targetBranch = rootBranch
			prURL = ""
		}
	}

	if prURL == "" {
		prInfo, infoErr := eng.GetBranch(targetBranch).GetPrInfo()
		if infoErr != nil || prInfo == nil || prInfo.URL() == "" {
			return fmt.Errorf("branch %q has no associated pull request; run `stackit submit` first", targetBranch)
		}
		prURL = prInfo.URL()
	}

	if err := utils.OpenBrowser(prURL); err != nil {
		return fmt.Errorf("failed to open %s in the browser: %w", prURL, err)
	}

	ctx.Output.Info("Opened %s", prURL)
	return nil
}

// resolvePRTarget resolves branchOrPR to a branch name. When branchOrPR is a
// PR number, it also returns the PR's URL directly from GitHub so callers
// don't need local metadata for a branch that may not be tracked.
func resolvePRTarget(ctx *app.Context, branchOrPR string) (branchName, prURL string, err error) {
	if branchOrPR == "" {
		name, err := ResolveBranchName(ctx.Engine, "")
		return name, "", err
	}

	prNum, convErr := strconv.Atoi(branchOrPR)
	if convErr != nil {
		return branchOrPR, "", nil
	}

	if _, err := ctx.RequireGitHub(); err != nil {
		return "", "", fmt.Errorf("cannot resolve PR #%d: %w", prNum, err)
	}
	remoteCtx, cancel := ctx.RemoteOperationContext()
	defer cancel()
	pr, err := ctx.GitHub().GetPullRequest(remoteCtx, prNum)
	if err != nil {
		return "", "", fmt.Errorf("failed to get PR #%d: %w", prNum, err)
	}
	return pr.Head, pr.HTMLURL, nil
}
