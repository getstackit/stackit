package git

import (
	"context"
	"fmt"
	"strings"
)

func (r *runner) CherryPick(ctx context.Context, commitSHA, onto string) (string, error) {
	if _, err := r.RunGitCommandWithContext(ctx, "checkout", "--detach", onto); err != nil {
		return "", fmt.Errorf("failed to checkout %s: %w", onto, err)
	}

	if _, err := r.RunGitCommandWithContext(ctx, "cherry-pick", commitSHA); err != nil {
		_, _ = r.RunGitCommandWithContext(ctx, "cherry-pick", "--abort")
		return "", fmt.Errorf("failed to cherry-pick %s: %w", commitSHA, err)
	}

	newSHA, err := r.RunGitCommandWithContext(ctx, "rev-parse", "HEAD")
	if err != nil {
		return "", fmt.Errorf("failed to get new SHA after cherry-pick: %w", err)
	}

	return strings.TrimSpace(newSHA), nil
}

func (r *runner) CherryPickSimple(ctx context.Context, commitSHA string) error {
	_, err := r.RunGitCommandWithContext(ctx, "cherry-pick", commitSHA)
	return err
}

func (r *runner) CherryPickAbort(ctx context.Context) error {
	_, err := r.RunGitCommandWithContext(ctx, "cherry-pick", "--abort")
	return err
}
