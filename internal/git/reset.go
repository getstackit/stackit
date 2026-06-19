package git

import (
	"context"
	"fmt"
)

func (r *runner) ResetMerge(ctx context.Context, revision string) error {
	err := r.resetWorktree(ctx, revision, "--merge")
	if err != nil {
		return fmt.Errorf("failed to reset --merge to %s: %w", revision, err)
	}
	return nil
}

func (r *runner) HardReset(ctx context.Context, revision string) error {
	err := r.resetWorktree(ctx, revision, "--hard")
	if err != nil {
		return fmt.Errorf("failed to hard reset to %s: %w", revision, err)
	}
	return nil
}

func (r *runner) SoftReset(ctx context.Context, revision string) error {
	err := r.resetWorktree(ctx, revision, "--soft")
	if err != nil {
		return fmt.Errorf("failed to soft reset to %s: %w", revision, err)
	}
	return nil
}

func (r *runner) MixedReset(ctx context.Context, revision string) error {
	return r.resetWorktree(ctx, revision, "--mixed")
}

// resetWorktree shells out to native git so the caller's context (deadline,
// cancellation) is honored.
func (r *runner) resetWorktree(ctx context.Context, revision, modeFlag string) error {
	if _, err := r.RunGitCommandWithContext(ctx, "reset", modeFlag, revision); err != nil {
		return err
	}
	return nil
}
