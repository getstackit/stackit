// Package validation provides reusable precondition validators for actions.
//
// Validators can be composed into chains for consistent validation:
//
//	err := validation.Chain{
//	    validation.MustBeOnBranch(eng),
//	    validation.MustNotHaveRebaseInProgress(git, ctx),
//	    validation.MustNotHaveUncommittedChanges(git, ctx),
//	}.Validate()
package validation

import (
	"context"
	"fmt"

	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/errors"
	"github.com/getstackit/stackit/internal/git"
)

// Validator validates a single precondition.
type Validator interface {
	// Validate checks the precondition and returns an error if it fails.
	Validate() error
}

// ValidatorFunc allows using a function as a Validator.
type ValidatorFunc func() error

// Validate implements Validator.
func (f ValidatorFunc) Validate() error {
	return f()
}

// Chain is a sequence of validators that are checked in order.
// Validation stops at the first error.
type Chain []Validator

// Validate runs all validators in sequence and returns the first error.
func (c Chain) Validate() error {
	for _, v := range c {
		if err := v.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// BranchValidationEngine is the minimal engine contract needed by validators.
// Keeping this narrow avoids coupling callers to the full engine.Engine surface.
type BranchValidationEngine interface {
	ValidateOnBranch() (string, error)
	GetBranch(branchName string) engine.Branch
	CurrentBranch() *engine.Branch
	AllBranches() engine.Branches
}

// MustBeOnBranch validates that HEAD is on a branch (not detached).
// Returns a Validator that checks the engine's current branch state.
func MustBeOnBranch(eng BranchValidationEngine) Validator {
	return ValidatorFunc(func() error {
		_, err := eng.ValidateOnBranch()
		return err
	})
}

// MustNotHaveRebaseInProgress validates that no rebase operation is in progress.
func MustNotHaveRebaseInProgress(ctx context.Context, g git.Runner) Validator {
	return ValidatorFunc(func() error {
		return g.CheckRebaseInProgress(ctx)
	})
}

// MustNotHaveUncommittedChanges validates that there are no uncommitted changes.
func MustNotHaveUncommittedChanges(ctx context.Context, g git.Runner) Validator {
	return ValidatorFunc(func() error {
		if g.HasUncommittedChanges(ctx) {
			return fmt.Errorf("cannot perform operation with uncommitted changes; please commit or stash them first")
		}
		return nil
	})
}

// MustHaveStagedChanges validates that there are staged changes ready to commit.
func MustHaveStagedChanges(ctx context.Context, g git.Runner) Validator {
	return ValidatorFunc(func() error {
		hasStagedChanges, err := g.HasStagedChanges(ctx)
		if err != nil {
			return fmt.Errorf("failed to check for staged changes: %w", err)
		}
		if !hasStagedChanges {
			return fmt.Errorf("no staged changes to commit; use 'git add' to stage changes")
		}
		return nil
	})
}

// BranchMustExist validates that a branch exists in git.
// Uses git.Runner to check if the branch ref exists.
func BranchMustExist(g git.Runner, branchName string) Validator {
	return ValidatorFunc(func() error {
		_, err := g.GetRevision(branchName)
		if err != nil {
			return fmt.Errorf("branch %s does not exist", branchName)
		}
		return nil
	})
}

// BranchMustBeTracked validates that a branch is tracked by stackit.
func BranchMustBeTracked(eng BranchValidationEngine, branchName string) Validator {
	return ValidatorFunc(func() error {
		branch := eng.GetBranch(branchName)
		if !branch.IsTracked() {
			return fmt.Errorf("branch %s is not tracked by stackit", branchName)
		}
		return nil
	})
}

// BranchMustBeModifiable validates that a branch can be modified (not locked, frozen, or worktree anchor).
func BranchMustBeModifiable(eng BranchValidationEngine, branchName string) Validator {
	return ValidatorFunc(func() error {
		branch := eng.GetBranch(branchName)
		return branch.EnsureCanModify()
	})
}

// BranchMustNotBeTrunk validates that a branch is not the trunk branch.
func BranchMustNotBeTrunk(eng BranchValidationEngine, branchName string) Validator {
	return ValidatorFunc(func() error {
		branch := eng.GetBranch(branchName)
		if branch.IsTrunk() {
			return fmt.Errorf("cannot perform operation on trunk branch")
		}
		return nil
	})
}

// CurrentBranchMustBeModifiable validates that the current branch can be modified.
func CurrentBranchMustBeModifiable(eng BranchValidationEngine) Validator {
	return ValidatorFunc(func() error {
		currentBranch := eng.CurrentBranch()
		if currentBranch == nil {
			return errors.ErrNotOnBranch
		}
		return currentBranch.EnsureCanModify()
	})
}

// CurrentBranchMustNotBeTrunk validates that the current branch is not trunk.
// The operation parameter is used in the error message (e.g., "reorder", "fold").
func CurrentBranchMustNotBeTrunk(eng BranchValidationEngine, operation string) Validator {
	return ValidatorFunc(func() error {
		currentBranch := eng.CurrentBranch()
		if currentBranch == nil {
			return errors.ErrNotOnBranch
		}
		if currentBranch.IsTrunk() {
			return fmt.Errorf("cannot %s trunk branch", operation)
		}
		return nil
	})
}

// CurrentBranchMustBeTracked validates that the current branch is tracked by stackit.
func CurrentBranchMustBeTracked(eng BranchValidationEngine) Validator {
	return ValidatorFunc(func() error {
		currentBranch := eng.CurrentBranch()
		if currentBranch == nil {
			return errors.ErrNotOnBranch
		}
		if !currentBranch.IsTracked() {
			return fmt.Errorf("branch %s is not tracked by stackit", currentBranch.GetName())
		}
		return nil
	})
}

// SourceBranchMustBeValid validates that a source branch can be used for operations
// like move, pluck, or delete. It checks:
//   - Branch is not trunk
//   - Branch is tracked by stackit
//   - Branch is not a worktree anchor
//
// The operation parameter is used in error messages (e.g., "move", "pluck", "delete").
func SourceBranchMustBeValid(eng BranchValidationEngine, branchName, operation string) Validator {
	return ValidatorFunc(func() error {
		return ValidateSourceBranch(eng, branchName, operation)
	})
}

// ValidateSourceBranch validates that a source branch can be used for operations
// like move, pluck, or delete. Returns nil if valid, error otherwise.
//
// Checks performed:
//   - Branch is not trunk
//   - Branch is tracked by stackit
//   - Branch is not a worktree anchor
//
// The operation parameter is used in error messages (e.g., "move", "pluck", "delete").
func ValidateSourceBranch(eng BranchValidationEngine, branchName, operation string) error {
	branch := eng.GetBranch(branchName)

	if branch.IsTrunk() {
		return fmt.Errorf("cannot %s trunk branch", operation)
	}

	if !branch.IsTracked() {
		return fmt.Errorf("branch %s is not tracked by stackit", branchName)
	}

	if branch.IsWorktreeAnchor() {
		return fmt.Errorf("cannot %s worktree anchor branch %s", operation, branchName)
	}

	return nil
}

// TargetBranchMustBeValid validates that a target branch can be used for reparenting
// operations like move or pluck. It checks:
//   - Target is provided (not empty)
//   - Target exists (as trunk, tracked, or untracked git branch)
//   - Target is not a worktree anchor
//   - Target is not the same as source
//
// The operation parameter is used in error messages (e.g., "move", "pluck").
func TargetBranchMustBeValid(eng BranchValidationEngine, sourceName, targetName, operation string) Validator {
	return ValidatorFunc(func() error {
		return ValidateTargetBranch(eng, sourceName, targetName, operation)
	})
}

// ValidateTargetBranch validates that a target branch can be used for reparenting
// operations like move or pluck. Returns nil if valid, error otherwise.
//
// Checks performed:
//   - Target is provided (not empty)
//   - Target exists (as trunk, tracked, or untracked git branch)
//   - Target is not a worktree anchor
//   - Target is not the same as source
//
// The operation parameter is used in error messages (e.g., "move", "pluck").
func ValidateTargetBranch(eng BranchValidationEngine, sourceName, targetName, operation string) error {
	if targetName == "" {
		return fmt.Errorf("target branch must be specified for %s", operation)
	}

	targetBranch := eng.GetBranch(targetName)

	// Target must exist - check if it's trunk, tracked, or at least a git branch
	if !targetBranch.IsTrunk() && !targetBranch.IsTracked() {
		allBranches := eng.AllBranches()
		found := false
		for _, branch := range allBranches {
			if branch.GetName() == targetName {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("branch %s does not exist", targetName)
		}
	}

	if targetBranch.IsWorktreeAnchor() {
		return fmt.Errorf("cannot %s branch onto worktree anchor %s; use 'stackit create' in the worktree instead", operation, targetName)
	}

	if sourceName == targetName {
		return fmt.Errorf("cannot %s branch onto itself", operation)
	}

	return nil
}
