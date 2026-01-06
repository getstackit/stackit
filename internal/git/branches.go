package git

import (
	"context"
	"fmt"
	"strings"
)

// detachedHEAD is the string returned by git rev-parse --abbrev-ref HEAD when in detached HEAD state
const detachedHEAD = "HEAD"

func (r *runner) GetCurrentBranch() (string, error) {
	branch, err := r.RunGitCommandWithContext(context.Background(), "rev-parse", "--abbrev-ref", detachedHEAD)
	if err != nil {
		return "", err
	}
	if branch == detachedHEAD {
		return "", fmt.Errorf("HEAD is not on a branch")
	}
	return branch, nil
}

func (r *runner) GetAllBranchNames() ([]string, error) {
	out, err := r.RunGitCommandWithContext(context.Background(), "branch", "--format=%(refname:short)")
	if err != nil {
		return nil, fmt.Errorf("failed to get branches: %w", err)
	}

	var names []string
	lines := strings.Split(out, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			names = append(names, line)
		}
	}
	return names, nil
}

func (r *runner) CheckoutBranch(ctx context.Context, branchName string) error {
	_, err := r.RunGitCommandWithContext(ctx, "checkout", branchName)
	if err != nil {
		return fmt.Errorf("failed to checkout branch %s: %w", branchName, err)
	}
	return nil
}

func (r *runner) CheckoutBranchForce(ctx context.Context, branchName string) error {
	_, err := r.RunGitCommandWithContext(ctx, "checkout", "-f", branchName)
	return err
}

func (r *runner) CreateAndCheckoutBranch(ctx context.Context, branchName string) error {
	_, err := r.RunGitCommandWithContext(ctx, "checkout", "-b", branchName)
	if err != nil {
		return fmt.Errorf("failed to create and checkout branch %s: %w", branchName, err)
	}
	return nil
}

func (r *runner) DeleteBranch(ctx context.Context, branchName string) error {
	_, err := r.RunGitCommandWithContext(ctx, "branch", "-D", branchName)
	if err != nil {
		return fmt.Errorf("failed to delete branch %s: %w", branchName, err)
	}
	return nil
}

func (r *runner) RenameBranch(ctx context.Context, oldName, newName string) error {
	_, err := r.RunGitCommandWithContext(ctx, "branch", "-m", oldName, newName)
	if err != nil {
		return fmt.Errorf("failed to rename branch %s to %s: %w", oldName, newName, err)
	}
	return nil
}

func (r *runner) CreateBranch(ctx context.Context, branchName, startPoint string) error {
	_, err := r.RunGitCommandWithContext(ctx, "branch", branchName, startPoint)
	if err != nil {
		return fmt.Errorf("failed to create branch %s from %s: %w", branchName, startPoint, err)
	}
	return nil
}

func (r *runner) CreateBranchForce(ctx context.Context, branchName, revision string) error {
	_, err := r.RunGitCommandWithContext(ctx, "branch", "-f", branchName, revision)
	return err
}

func (r *runner) CheckoutDetached(ctx context.Context, revision string) error {
	_, err := r.RunGitCommandWithContext(ctx, "checkout", "--detach", revision)
	if err != nil {
		return fmt.Errorf("failed to checkout %s in detached state: %w", revision, err)
	}
	return nil
}

func (r *runner) UpdateBranchRef(ctx context.Context, branchName, revision string) error {
	_, err := r.RunGitCommandWithContext(ctx, "update-ref", "refs/heads/"+branchName, revision)
	if err != nil {
		return fmt.Errorf("failed to update branch ref: %w", err)
	}
	return nil
}

func (r *runner) GetCurrentBranchOrSHA(ctx context.Context) (string, error) {
	branch, err := r.RunGitCommandWithContext(ctx, "rev-parse", "--abbrev-ref", "HEAD")
	if err == nil && branch != "HEAD" {
		return branch, nil
	}
	return r.GetCurrentRevision(ctx)
}

func (r *runner) GetMergedBranches(ctx context.Context, target string) (map[string]bool, error) {
	out, err := r.RunGitCommandWithContext(ctx, "branch", "--merged", target)
	if err != nil {
		return nil, fmt.Errorf("failed to get merged branches: %w", err)
	}

	merged := make(map[string]bool)
	lines := strings.Split(out, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Remove current branch indicator '*' if present
		line = strings.TrimPrefix(line, "*")
		line = strings.TrimSpace(line)
		merged[line] = true
	}
	return merged, nil
}
