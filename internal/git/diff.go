package git

import (
	"context"
	"fmt"
	"strings"
)

func (r *runner) IsDiffEmpty(ctx context.Context, branchName, base string) (bool, error) {
	branchRev, err := r.GetRevision(branchName)
	if err != nil {
		return false, fmt.Errorf("failed to get branch revision: %w", err)
	}

	if branchRev == base {
		return true, nil
	}

	_, err = r.RunGitCommandWithContext(ctx, "diff", "--quiet", base, branchRev)
	return err == nil, nil
}

func (r *runner) GetChangedFiles(ctx context.Context, base, head string) ([]string, error) {
	output, err := r.RunGitCommandWithContext(ctx, "diff", "--name-only", base, head)
	if err != nil {
		return nil, fmt.Errorf("failed to get changed files: %w", err)
	}
	if output == "" {
		return []string{}, nil
	}
	return strings.Split(strings.TrimSpace(output), "\n"), nil
}

func (r *runner) ShowDiff(ctx context.Context, left, right string, stat bool) (string, error) {
	args := []string{"-c", "color.ui=always", "--no-pager", "diff", "--no-ext-diff"}
	if stat {
		args = append(args, "--stat")
	}
	args = append(args, left, right, "--")
	return r.RunGitCommandWithContext(ctx, args...)
}

func (r *runner) ShowCommits(ctx context.Context, base, head string, patch, stat bool) (string, error) {
	args := []string{"-c", "color.ui=always", "--no-pager", "log"}
	switch {
	case patch && stat:
		args = append(args, "--stat")
	case patch:
		args = append(args, "-p")
	default:
		args = append(args, "--pretty=format:%h - %s")
	}

	// If base is empty, use head~ (parent commit) for trunk
	baseRef := base
	if base == "" {
		baseRef = head + "~"
	}
	args = append(args, fmt.Sprintf("%s..%s", baseRef, head))
	args = append(args, "--")
	return r.RunGitCommandWithContext(ctx, args...)
}

func (r *runner) GetStagedDiff(ctx context.Context, files ...string) (string, error) {
	args := []string{"diff", "--cached"}
	if len(files) > 0 {
		args = append(args, "--")
		args = append(args, files...)
	}
	return r.RunGitCommandRawWithContext(ctx, args...)
}

func (r *runner) GetUnstagedDiff(ctx context.Context, files ...string) (string, error) {
	args := []string{"diff"}
	if len(files) > 0 {
		args = append(args, "--")
		args = append(args, files...)
	}
	return r.RunGitCommandRawWithContext(ctx, args...)
}
