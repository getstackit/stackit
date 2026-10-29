package validation

import (
	"context"
)

// GitOperationEngine is the engine contract needed by the git-state chains:
// branch validation plus git-state reads. engine.Engine satisfies it, so
// callers pass the engine rather than the raw git runner.
type GitOperationEngine interface {
	BranchValidationEngine
	GitStateReader
}

// ModifyBranchChain validates for simple branch modifications (rename, scope).
// Checks: on branch, not trunk, modifiable.
func ModifyBranchChain(ctx context.Context, eng BranchValidationEngine, operation string) Chain {
	return Chain{
		MustBeOnBranch(ctx, eng),
		CurrentBranchMustNotBeTrunk(ctx, eng, operation),
		CurrentBranchMustBeModifiable(ctx, eng),
	}
}

// GitOperationChain validates for git history modifications (fold, pop, reorder).
// Checks: on branch, not trunk, tracked, no rebase in progress, no uncommitted changes, modifiable.
func GitOperationChain(ctx context.Context, eng GitOperationEngine, operation string) Chain {
	return Chain{
		MustBeOnBranch(ctx, eng),
		CurrentBranchMustNotBeTrunk(ctx, eng, operation),
		CurrentBranchMustBeTracked(ctx, eng),
		MustNotHaveRebaseInProgress(ctx, eng),
		MustNotHaveUncommittedChanges(ctx, eng),
		CurrentBranchMustBeModifiable(ctx, eng),
	}
}

// AbsorbChain validates for absorb operations (works with staged changes).
// Checks: on branch, not trunk, modifiable, no rebase in progress.
func AbsorbChain(ctx context.Context, eng GitOperationEngine, operation string) Chain {
	return Chain{
		MustBeOnBranch(ctx, eng),
		CurrentBranchMustNotBeTrunk(ctx, eng, operation),
		CurrentBranchMustBeTracked(ctx, eng),
		CurrentBranchMustBeModifiable(ctx, eng),
		MustNotHaveRebaseInProgress(ctx, eng),
	}
}
