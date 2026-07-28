package actions

import (
	"fmt"
	"strconv"

	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/errors"
	"github.com/getstackit/stackit/internal/output"
	"github.com/getstackit/stackit/internal/utils"
)

// PrOptions configures which pull request PrAction opens.
type PrOptions struct {
	// BranchOrPR selects the target: a branch name, a PR number, or empty
	// for the current branch.
	BranchOrPR string
	// Stack, when true, resolves the root PR of the stack containing the
	// target branch instead of the target branch's own PR.
	Stack bool
}

// PrResult is the pull request PrAction resolved and opened.
type PrResult struct {
	BranchName string
	PRNumber   int
	URL        string
}

// PrAction resolves the pull request for a branch, a PR number, or the
// current branch, optionally walking up to the root PR of the stack, and
// opens it in the default browser.
func PrAction(ctx *app.Context, opts PrOptions) (*PrResult, error) {
	eng := ctx.Engine

	branchName, result, err := resolvePrTarget(ctx, opts.BranchOrPR)
	if err != nil {
		return nil, err
	}

	if opts.Stack {
		branchName, result, err = resolveStackRootPr(eng, branchName)
		if err != nil {
			return nil, err
		}
	}

	if err := utils.OpenBrowser(result.URL); err != nil {
		ctx.Output.Debug("failed to open browser: %v", err)
	}
	ctx.Output.Success("Opening %s (%s) in your browser: %s", output.PRNumber(result.PRNumber), output.BranchName(branchName), result.URL)

	result.BranchName = branchName
	return result, nil
}

// resolvePrTarget resolves branchOrPR (a branch name, a PR number, or empty
// for the current branch) to a branch name and its pull request. PR numbers
// are resolved via the GitHub API so branches never fetched locally (e.g. a
// plain GitHub branch that never went through `stackit create`) still work.
func resolvePrTarget(ctx *app.Context, branchOrPR string) (string, *PrResult, error) {
	eng := ctx.Engine

	if branchOrPR == "" {
		current := eng.CurrentBranch()
		if current == nil {
			return "", nil, errors.ErrNotOnBranchNoBranchSpecified
		}
		return resolveTrackedBranchPr(eng, current.GetName())
	}

	if prNumber, err := strconv.Atoi(branchOrPR); err == nil {
		gh, err := ctx.RequireGitHub()
		if err != nil {
			return "", nil, fmt.Errorf("cannot resolve PR #%d: %w", prNumber, err)
		}
		pr, err := gh.GetPullRequest(ctx.Context, prNumber)
		if err != nil {
			return "", nil, fmt.Errorf("failed to get PR #%d: %w", prNumber, err)
		}
		return pr.Head, &PrResult{PRNumber: pr.Number, URL: pr.HTMLURL}, nil
	}

	return resolveTrackedBranchPr(eng, branchOrPR)
}

// resolveStackRootPr walks up from branchName to the root of its stack (the
// branch directly above trunk) and resolves that branch's pull request.
func resolveStackRootPr(eng engine.Engine, branchName string) (string, *PrResult, error) {
	root := eng.GetStackRootForBranch(eng.GetBranch(branchName))
	if root == "" {
		return "", nil, fmt.Errorf("%q is not part of a tracked stack", branchName)
	}
	return resolveTrackedBranchPr(eng, root)
}

// resolveTrackedBranchPr looks up the pull request for a branch tracked in
// local stackit metadata.
func resolveTrackedBranchPr(eng engine.Engine, branchName string) (string, *PrResult, error) {
	branchObj := eng.GetBranch(branchName)
	if branchObj.IsTrunk() {
		return "", nil, fmt.Errorf("%q is the trunk branch and has no pull request", branchName)
	}
	if !branchObj.IsTracked() {
		return "", nil, fmt.Errorf("%q is not tracked by stackit; run `stackit create` or `stackit track` first", branchName)
	}

	prInfo, err := branchObj.GetPrInfo()
	if err != nil {
		return "", nil, fmt.Errorf("failed to read pull request info for %q: %w", branchName, err)
	}
	if prInfo == nil || prInfo.Number() == nil || prInfo.URL() == "" {
		return "", nil, fmt.Errorf("%q has no associated pull request; run `stackit submit` first", branchName)
	}

	return branchName, &PrResult{PRNumber: *prInfo.Number(), URL: prInfo.URL()}, nil
}
