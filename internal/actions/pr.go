package actions

import (
	"fmt"
	"strconv"

	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/errors"
	"github.com/getstackit/stackit/internal/utils"
)

// OpenPROptions configures which pull request to open.
type OpenPROptions struct {
	// Target is a branch name or PR number from the command line. Empty means
	// the current branch.
	Target string
	// Stack opens the root PR of the target's stack instead of the target
	// branch's own PR.
	Stack bool
}

// OpenPRAction resolves the pull request opts identifies and opens it in the
// default browser.
func OpenPRAction(ctx *app.Context, opts OpenPROptions) error {
	eng := ctx.Engine

	branchName, err := resolvePRBranch(eng, opts.Target)
	if err != nil {
		return err
	}

	if opts.Stack {
		if root := eng.GetStackRootForBranch(eng.GetBranch(branchName)); root != "" {
			branchName = root
		}
	}

	prInfo, err := eng.GetBranch(branchName).GetPrInfo()
	if err != nil {
		return fmt.Errorf("reading pull request info for %q: %w", branchName, err)
	}
	if prInfo == nil || prInfo.URL() == "" {
		return fmt.Errorf("%q has no associated pull request; run 'stackit submit' first", branchName)
	}

	ctx.Output.Info("Opening %s", prInfo.URL())
	// Best-effort: environments without a browser (SSH, headless CI) still get
	// the URL printed above, mirroring submit's --web behavior.
	if err := utils.OpenBrowser(prInfo.URL()); err != nil {
		ctx.Output.Debug("Failed to open browser: %v", err)
	}
	return nil
}

// resolvePRBranch turns target into a branch name. An empty target resolves to
// the current branch; a numeric target resolves to whichever tracked branch
// has that PR number; anything else is used as a branch name directly.
func resolvePRBranch(eng engine.Engine, target string) (string, error) {
	if target == "" {
		current := eng.CurrentBranch()
		if current == nil {
			return "", errors.ErrNotOnBranch
		}
		return current.GetName(), nil
	}

	number, convErr := strconv.Atoi(target)
	if convErr != nil {
		if !eng.BranchNames().Contains(target) {
			return "", fmt.Errorf("branch %q not found: %w", target, errors.ErrBranchNotFound)
		}
		return target, nil
	}

	branchName, found := findBranchByPRNumber(eng, number)
	if !found {
		return "", fmt.Errorf("no tracked branch found for pull request #%d", number)
	}
	return branchName, nil
}

// findBranchByPRNumber searches tracked branches for the one whose recorded PR
// number matches number, reading all metadata in a single batched pass rather
// than once per branch.
func findBranchByPRNumber(eng engine.Engine, number int) (string, bool) {
	branchNames := eng.AllBranches().Names()
	metas, _ := eng.BatchReadMetadataRaw(branchNames)
	for _, name := range branchNames {
		meta := metas[name]
		if meta == nil {
			continue
		}
		prInfo := meta.GetPrInfo()
		if prInfo != nil && prInfo.Number != nil && *prInfo.Number == number {
			return name, true
		}
	}
	return "", false
}
