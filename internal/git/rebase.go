package git

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// RebaseResult represents the result of a rebase operation
type RebaseResult int

const (
	// RebaseDone indicates the rebase was successful
	RebaseDone RebaseResult = iota
	// RebaseConflict indicates a conflict occurred during rebase
	RebaseConflict
)

// Rebase rebases a branch onto another branch.
// onto is the branch name to rebase onto (parent branch).
// from is the base revision (old parent branch revision).
func Rebase(ctx context.Context, branchName, onto, from string) (RebaseResult, error) {
	// Use detached HEAD to avoid "already used by worktree" errors
	// git rebase --onto <onto> <from> <branchName>
	_, err := RunGitCommandWithContext(ctx, "rebase", "--onto", onto, from, branchName)
	if err != nil {
		if IsRebaseInProgress(ctx) {
			return RebaseConflict, nil
		}
		// Abort rebase if it failed for other reasons
		_, _ = RunGitCommandWithContext(ctx, "rebase", "--abort")

		return RebaseConflict, nil
	}

	return RebaseDone, nil
}

// CherryPick cherry-picks a commit onto another revision
func CherryPick(ctx context.Context, commitSHA, onto string) (string, error) {
	if _, err := RunGitCommandWithContext(ctx, "checkout", "--detach", onto); err != nil {
		return "", fmt.Errorf("failed to checkout %s: %w", onto, err)
	}

	if _, err := RunGitCommandWithContext(ctx, "cherry-pick", commitSHA); err != nil {
		_, _ = RunGitCommandWithContext(ctx, "cherry-pick", "--abort")
		return "", fmt.Errorf("failed to cherry-pick %s: %w", commitSHA, err)
	}

	newSHA, err := RunGitCommandWithContext(ctx, "rev-parse", "HEAD")
	if err != nil {
		return "", fmt.Errorf("failed to get new SHA after cherry-pick: %w", err)
	}

	return strings.TrimSpace(newSHA), nil
}

// RebaseContinue continues an in-progress rebase
func RebaseContinue(ctx context.Context) (RebaseResult, error) {
	_, err := RunGitCommandWithEnv(ctx, []string{"GIT_EDITOR=true"}, "rebase", "--continue")
	if err != nil {
		if IsRebaseInProgress(ctx) {
			return RebaseConflict, nil
		}
		return RebaseConflict, fmt.Errorf("rebase continue failed: %w", err)
	}

	return RebaseDone, nil
}

// RebaseAbort aborts an in-progress rebase
func RebaseAbort(ctx context.Context) error {
	_, err := RunGitCommandWithContext(ctx, "rebase", "--abort")
	if err != nil {
		return fmt.Errorf("rebase abort failed: %w", err)
	}
	return nil
}

// IsRebaseInProgress checks if a rebase is currently in progress
func IsRebaseInProgress(_ context.Context) bool {
	// Check for .git/rebase-merge or .git/rebase-apply directories
	// Use the working directory to construct paths
	workingDir := GetWorkingDir()
	if workingDir == "" {
		return false
	}

	gitDir := filepath.Join(workingDir, ".git")

	// Check for interactive rebase
	if _, err := os.Stat(filepath.Join(gitDir, "rebase-merge")); err == nil {
		return true
	}
	// Check for non-interactive rebase
	if _, err := os.Stat(filepath.Join(gitDir, "rebase-apply")); err == nil {
		return true
	}

	return false
}

// GetRebaseHead returns the commit being rebased (REBASE_HEAD)
func GetRebaseHead() (string, error) {
	// Try standard rebase head refs in order:
	// 1. REBASE_HEAD (standard)
	// 2. refs/rebase-merge/head (interactive)
	// 3. refs/rebase-apply/head (non-interactive)
	refs := []string{
		"REBASE_HEAD",
		"refs/rebase-merge/head",
		"refs/rebase-apply/head",
	}

	for _, refName := range refs {
		output, err := RunGitCommand("rev-parse", "--verify", refName)
		if err == nil && output != "" {
			return strings.TrimSpace(output), nil
		}
	}

	return "", fmt.Errorf("rebase head not found")
}
