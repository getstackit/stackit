package actions

import (
	"context"
	"fmt"

	"github.com/getstackit/stackit/internal/engine"
)

// rewriteGuardEngine is the narrow engine surface RewriteGuard needs to put a
// branch back after a failed rewrite.
type rewriteGuardEngine interface {
	ForceCheckoutBranch(ctx context.Context, branch engine.Branch) error
	UpdateBranchRef(ctx context.Context, branchName, revision string) error
}

// detachForRewriteEngine adds the detach step used to start a rewrite.
type detachForRewriteEngine interface {
	rewriteGuardEngine
	DetachAndResetBranchChanges(ctx context.Context, branchName string) error
}

// RewriteGuard restores the original branch when a branch-rewriting operation
// fails, so the user is never left on a detached HEAD.
//
// Typical use:
//
//	guard, err := actions.DetachForRewrite(ctx, eng, branch)
//	if err != nil {
//		return err
//	}
//	defer guard.RestoreUnlessReleased(ctx, &err) // err is a named result
//	...
//	guard.Release()
//	return nil
//
// Restoring runs, in order: cleanup hooks registered with OnRestore (last
// registered first), a reset of the branch ref when RestoreRefTo was called,
// then a force checkout of the branch. A restore failure never masks the
// operation's error: it is appended to it along with a recovery command.
type RewriteGuard struct {
	eng         rewriteGuardEngine
	branch      engine.Branch
	originalSHA string
	hooks       []func()
	released    bool
}

// DetachForRewrite detaches HEAD and resets the branch's changes into the
// working tree, returning a guard that restores the branch on failure.
func DetachForRewrite(ctx context.Context, eng detachForRewriteEngine, branch engine.Branch) (*RewriteGuard, error) {
	if err := eng.DetachAndResetBranchChanges(ctx, branch.GetName()); err != nil {
		return nil, fmt.Errorf("failed to detach and reset: %w", err)
	}
	return NewRewriteGuard(eng, branch), nil
}

// NewRewriteGuard returns a guard for an operation that detaches HEAD itself
// (for example engine.ApplySplitToCommits).
func NewRewriteGuard(eng rewriteGuardEngine, branch engine.Branch) *RewriteGuard {
	return &RewriteGuard{eng: eng, branch: branch}
}

// OnRestore registers cleanup that must run before the branch is restored,
// such as popping a stash or deleting a partially created branch. Hooks run in
// reverse registration order, like defers.
func (g *RewriteGuard) OnRestore(hook func()) {
	g.hooks = append(g.hooks, hook)
}

// RestoreRefTo records the SHA the branch ref must be reset to on restore. Call
// it once the operation has moved the branch ref.
func (g *RewriteGuard) RestoreRefTo(sha string) {
	g.originalSHA = sha
}

// Release disarms the guard. Call it once the rewrite has succeeded, or once it
// has reached a state that must be left in place for manual recovery.
func (g *RewriteGuard) Release() {
	g.released = true
}

// Restore puts the branch back now and returns opErr, annotated with recovery
// guidance if restoring failed. The guard is released afterwards so a deferred
// RestoreUnlessReleased does not restore twice.
func (g *RewriteGuard) Restore(ctx context.Context, opErr error) error {
	g.released = true

	for i := len(g.hooks) - 1; i >= 0; i-- {
		g.hooks[i]()
	}

	name := g.branch.GetName()
	if g.originalSHA != "" {
		if err := g.eng.UpdateBranchRef(ctx, name, g.originalSHA); err != nil {
			return withRecoveryWarning(opErr, fmt.Sprintf("failed to restore branch ref: %s; run 'git branch -f %s %s' to recover",
				err.Error(), name, g.originalSHA))
		}
	}

	if err := g.eng.ForceCheckoutBranch(ctx, g.branch); err != nil {
		return withRecoveryWarning(opErr, fmt.Sprintf("failed to restore to %s: %s; run 'git checkout %s' to recover",
			name, err.Error(), name))
	}
	return opErr
}

// RestoreUnlessReleased restores the branch unless Release was called. Pass a
// pointer to the caller's named error result; a restore failure is added to it.
func (g *RewriteGuard) RestoreUnlessReleased(ctx context.Context, errp *error) {
	if g.released {
		return
	}
	*errp = g.Restore(ctx, *errp)
}

// withRecoveryWarning appends a restore failure to the operation's error. When
// the operation itself reported no error, the warning becomes the error: the
// user was left off their branch and needs to know.
func withRecoveryWarning(opErr error, warning string) error {
	if opErr == nil {
		return fmt.Errorf("WARNING: %s", warning)
	}
	return fmt.Errorf("%w (WARNING: %s)", opErr, warning)
}
