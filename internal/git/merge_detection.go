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
	r.revisionCache.InvalidateAll()
	return nil
}

// MergeMultiple performs an octopus merge of multiple branches into the current branch.
// This creates a single merge commit with multiple parents.
func (r *runner) MergeMultiple(ctx context.Context, branches []string, opts MergeOptions) error {
	if len(branches) == 0 {
		return nil
	}

	args := []string{"merge"}
	if opts.NoEdit {
		args = append(args, "--no-edit")
	}
	if opts.NoFF {
		args = append(args, "--no-ff")
	}
	if opts.Message != "" {
		args = append(args, "-m", opts.Message)
	}
	args = append(args, branches...)

	_, err := r.RunGitCommandWithContext(ctx, args...)
	if err != nil {
		return fmt.Errorf("failed to merge branches: %w", err)
	}
	r.revisionCache.InvalidateAll()
	return nil
}

func (r *runner) IsMergeInProgress(ctx context.Context) bool {
	gitDir := r.getGitDir(ctx)
	mergeHead := filepath.Join(gitDir, "MERGE_HEAD")
	if _, err := os.Stat(mergeHead); err == nil {
		return true
	}
	return false
}

func (r *runner) MergeAbort(ctx context.Context) error {
	_, err := r.RunGitCommandWithContext(ctx, "merge", "--abort")
	r.revisionCache.InvalidateAll()
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
	mergeBase, err := r.GetMergeBase(ctx, branchName, target)
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
		return r.IsAncestor(ctx, branchRev, target)
	}

	// If cherry output is empty or all lines start with '-', branch is merged
	if cherryOutput == "" {
		return true, nil
	}

	// Check if all commits are marked as merged (lines starting with '-')
	lines := strings.SplitSeq(strings.TrimSpace(cherryOutput), "\n")
	for line := range lines {
		if line != "" && line[0] != '-' {
			return false, nil
		}
	}

	return true, nil
}
