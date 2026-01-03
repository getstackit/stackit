package git

import (
	"context"
	"fmt"
	"strings"
)

func (r *runner) AddWorktree(ctx context.Context, path string, branch string, detach bool) error {
	args := []string{"worktree", "add"}
	if detach {
		args = append(args, "--detach")
	}
	args = append(args, path)
	if branch != "" {
		args = append(args, branch)
	}

	_, err := r.RunGitCommandWithContext(ctx, args...)
	if err != nil {
		return fmt.Errorf("failed to add worktree at %s: %w", path, err)
	}
	return nil
}

func (r *runner) RemoveWorktree(ctx context.Context, path string) error {
	_, err := r.RunGitCommandWithContext(ctx, "worktree", "remove", "--force", path)
	if err != nil {
		return fmt.Errorf("failed to remove worktree at %s: %w", path, err)
	}
	return nil
}

func (r *runner) ListWorktrees(ctx context.Context) ([]string, error) {
	output, err := r.RunGitCommandWithContext(ctx, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, fmt.Errorf("failed to list worktrees: %w", err)
	}

	if output == "" {
		return []string{}, nil
	}

	lines := strings.Split(strings.TrimSpace(output), "\n")
	var worktrees []string
	for _, line := range lines {
		if len(line) > 9 && line[:9] == "worktree " {
			worktrees = append(worktrees, line[9:])
		}
	}
	return worktrees, nil
}
