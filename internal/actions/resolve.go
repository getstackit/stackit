package actions

import (
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/errors"
)

// ResolveBranchName resolves a branch name, defaulting to current branch if empty.
// Returns errors.ErrNotOnBranchNoBranchSpecified if no branch specified and not on a branch.
func ResolveBranchName(eng engine.BranchReader, branchName string) (string, error) {
	if branchName != "" {
		return branchName, nil
	}
	currentBranch := eng.CurrentBranch()
	if currentBranch == nil {
		return "", errors.ErrNotOnBranchNoBranchSpecified
	}
	return currentBranch.GetName(), nil
}

// ResolveBranch resolves a branch name to a Branch, defaulting to current branch if empty.
// Returns errors.ErrNotOnBranchNoBranchSpecified if no branch specified and not on a branch.
func ResolveBranch(eng engine.BranchReader, branchName string) (engine.Branch, error) {
	name, err := ResolveBranchName(eng, branchName)
	if err != nil {
		return engine.Branch{}, err
	}
	return eng.GetBranch(name), nil
}
