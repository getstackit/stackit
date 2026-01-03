package git

import (
	"context"
	"fmt"
)

func (r *runner) ResetMerge(ctx context.Context, revision string) error {
	_, err := r.runGitCommandWithContextInternal(ctx, "reset", "--merge", revision)
	if err != nil {
		return fmt.Errorf("failed to reset --merge to %s: %w", revision, err)
	}
	return nil
}

func (r *runner) HardReset(ctx context.Context, revision string) error {
	_, err := r.runGitCommandWithContextInternal(ctx, "reset", "--hard", revision)
	if err != nil {
		return fmt.Errorf("failed to hard reset to %s: %w", revision, err)
	}
	return nil
}

func (r *runner) SoftReset(ctx context.Context, revision string) error {
	_, err := r.runGitCommandWithContextInternal(ctx, "reset", "-q", "--soft", revision)
	if err != nil {
		return fmt.Errorf("failed to soft reset to %s: %w", revision, err)
	}
	return nil
}

func (r *runner) MixedReset(ctx context.Context, revision string) error {
	_, err := r.runGitCommandWithContextInternal(ctx, "reset", revision)
	return err
}
