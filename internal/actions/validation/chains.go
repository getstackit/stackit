package validation

import (
	"context"

	"github.com/getstackit/stackit/internal/git"
)

// ModifyBranchChain validates for simple branch modifications (rename, scope).
// Checks: on branch, not trunk, modifiable.
func ModifyBranchChain(eng BranchValidationEngine, operation string) Chain {
	return Chain{
		MustBeOnBranch(eng),
		CurrentBranchMustNotBeTrunk(eng, operation),
		CurrentBranchMustBeModifiable(eng),
	}
}

// GitOperationChain validates for git history modifications (fold, pop, reorder).
// Checks: on branch, not trunk, tracked, no rebase in progress, no uncommitted changes, modifiable.
func GitOperationChain(ctx context.Context, eng BranchValidationEngine, g git.Runner, operation string) Chain {
	return Chain{
		MustBeOnBranch(eng),
		CurrentBranchMustNotBeTrunk(eng, operation),
		CurrentBranchMustBeTracked(eng),
		MustNotHaveRebaseInProgress(ctx, g),
		MustNotHaveUncommittedChanges(ctx, g),
		CurrentBranchMustBeModifiable(eng),
	}
}

// AbsorbChain validates for absorb operations (works with staged changes).
// Checks: on branch, not trunk, modifiable, no rebase in progress.
func AbsorbChain(ctx context.Context, eng BranchValidationEngine, g git.Runner, operation string) Chain {
	return Chain{
		MustBeOnBranch(eng),
		CurrentBranchMustNotBeTrunk(eng, operation),
		CurrentBranchMustBeModifiable(eng),
		MustNotHaveRebaseInProgress(ctx, g),
	}
}
