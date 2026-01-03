package git

import (
	"context"
	"fmt"
)

func (r *runner) StashPush(ctx context.Context, message string) (string, error) {
	args := []string{"stash", "push", "-u"}
	if message != "" {
		args = append(args, "-m", message)
	}
	output, err := r.RunGitCommandWithContext(ctx, args...)
	if err != nil {
		return "", fmt.Errorf("stash push failed: %w", err)
	}
	return output, nil
}

func (r *runner) StashPop(ctx context.Context) error {
	_, err := r.RunGitCommandWithContext(ctx, "stash", "pop")
	if err != nil {
		return fmt.Errorf("stash pop failed: %w", err)
	}
	return nil
}

func (r *runner) ListStash(ctx context.Context) (string, error) {
	return r.RunGitCommandWithContext(ctx, "stash", "list")
}
