package actions

import (
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/errors"
	"github.com/getstackit/stackit/internal/git"
)

// CurrentBranchReader is the minimal dependency needed to resolve an omitted
// branch argument.
type CurrentBranchReader interface {
	CurrentBranch() *engine.Branch
}

// ResolveBranchName resolves a branch name, defaulting to current branch if empty.
// Returns errors.ErrNotOnBranchNoBranchSpecified if no branch specified and not on a branch.
func ResolveBranchName(eng CurrentBranchReader, branchName string) (string, error) {
	if branchName != "" {
		return branchName, nil
	}
	currentBranch := eng.CurrentBranch()
	if currentBranch == nil {
		return "", errors.ErrNotOnBranchNoBranchSpecified
	}
	return currentBranch.GetName(), nil
}

// PRNumberForBranch returns the submitted PR number for a branch, when one is
// available in local metadata. Read failures are intentionally treated as an
// unknown number because progress reporting must not fail an operation.
func PRNumberForBranch(eng engine.BranchReader, branchName string) *git.PRNumber {
	prInfo, err := eng.GetPrInfo(eng.GetBranch(branchName))
	if err != nil || prInfo == nil {
		return nil
	}
	return prInfo.Number()
}
