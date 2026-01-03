package git

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func (r *runner) Merge(ctx context.Context, branchName string, opts MergeOptions) error {
	args := []string{"merge"}
	if opts.FFOnly {
		args = append(args, "--ff-only")
	}
	if opts.NoEdit {
		args = append(args, "--no-edit")
	}
	if opts.NoFF {
		args = append(args, "--no-ff")
	}
	if opts.Message != "" {
		args = append(args, "-m", opts.Message)
	}
	args = append(args, branchName)

	_, err := r.RunGitCommandWithContext(ctx, args...)
	if err != nil {
		return fmt.Errorf("failed to merge %s: %w", branchName, err)
	}
	return nil
}

func (r *runner) IsMergeInProgress(_ context.Context) bool {
	if r.repoRoot == "" {
		return false
	}
	mergeHead := filepath.Join(r.repoRoot, ".git", "MERGE_HEAD")
	if _, err := os.Stat(mergeHead); err == nil {
		return true
	}
	return false
}

func (r *runner) MergeAbort(ctx context.Context) error {
	_, err := r.RunGitCommandWithContext(ctx, "merge", "--abort")
	if err != nil {
		return fmt.Errorf("merge abort failed: %w", err)
	}
	return nil
}

func (r *runner) GetUnmergedFiles(ctx context.Context) ([]string, error) {
	output, err := r.RunGitCommandWithContext(ctx, "diff", "--name-only", "--diff-filter=U")
	if err != nil {
		return []string{}, nil //nolint:nilerr
	}
	if output == "" {
		return []string{}, nil
	}
	return strings.Split(strings.TrimSpace(output), "\n"), nil
}

func (r *runner) IsMerged(ctx context.Context, branchName, target string) (bool, error) {
	// Get merge base
	mergeBase, err := r.GetMergeBase(branchName, target)
	if err != nil {
		return false, fmt.Errorf("failed to get merge base: %w", err)
	}

	// Get branch revision
	branchRev, err := r.GetRevision(branchName)
	if err != nil {
		return false, fmt.Errorf("failed to get branch revision: %w", err)
	}

	// If merge base equals branch revision, branch is already merged
	if mergeBase == branchRev {
		return true, nil
	}

	// Use git cherry to check if all commits are in trunk
	// git cherry <trunk> <branch> returns commits that are in branch but not in trunk
	// If empty, all commits are merged
	cherryOutput, err := r.RunGitCommandWithContext(ctx, "cherry", target, branchName)
	if err != nil {
		// If cherry fails, fall back to simpler check
		// Check if branch tip is reachable from trunk
		return r.IsAncestor(branchRev, target)
	}

	// If cherry output is empty or all lines start with '-', branch is merged
	if cherryOutput == "" {
		return true, nil
	}

	// Check if all commits are marked as merged (lines starting with '-')
	lines := strings.Split(strings.TrimSpace(cherryOutput), "\n")
	for _, line := range lines {
		if line != "" && line[0] != '-' {
			return false, nil
		}
	}

	return true, nil
}
