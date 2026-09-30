package engine

import (
	"context"

	"github.com/getstackit/stackit/internal/git"
)

// Commit creates a new commit with the given options.
func (e *engineImpl) Commit(_ context.Context, opts git.CommitOptions) error {
	return e.git.CommitWithOptions(opts)
}

// StageAll stages all changes
func (e *engineImpl) StageAll(ctx context.Context) error {
	return e.git.StageAll(ctx)
}

// StagePatch stages changes interactively
func (e *engineImpl) StagePatch(ctx context.Context) error {
	return e.git.StagePatch(ctx)
}

// StageHunks stages specific hunks by applying them as patches
func (e *engineImpl) StageHunks(ctx context.Context, hunks []git.Hunk) error {
	return e.git.StageHunks(ctx, hunks)
}

// ApplyPatchToWorktree applies a patch to the working tree only (not the index).
func (e *engineImpl) ApplyPatchToWorktree(ctx context.Context, patch string) error {
	return e.git.ApplyPatchToWorktree(ctx, patch)
}

// StageChanges stages changes according to the given staging options.
func (e *engineImpl) StageChanges(ctx context.Context, opts git.StagingOptions) error {
	return e.git.StageChanges(ctx, opts)
}

// StashPush pushes current changes to the stash
func (e *engineImpl) StashPush(ctx context.Context, message string) (string, error) {
	return e.git.StashPush(ctx, message)
}

// StashPushStaged pushes only staged changes to the stash
func (e *engineImpl) StashPushStaged(ctx context.Context, message string) (string, error) {
	return e.git.StashPushStaged(ctx, message)
}

// StashDrop drops a stash entry by ref.
func (e *engineImpl) StashDrop(ctx context.Context, ref string) error {
	return e.git.StashDrop(ctx, ref)
}

// StashPopRef pops a specific stash entry by ref.
func (e *engineImpl) StashPopRef(ctx context.Context, ref string) error {
	return e.git.StashPopRef(ctx, ref)
}
