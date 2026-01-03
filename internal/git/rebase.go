package git

import (
	"context"
	"fmt"
	"os"
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

func (r *runner) Rebase(ctx context.Context, branchName, upstream, oldUpstream string) (RebaseResult, error) {
	// Use detached HEAD to avoid "already used by worktree" errors
	// We use branchName~0 to force a detached checkout of the branch tip
	_, err := r.runGitCommandWithContextInternal(ctx, "rebase", "--onto", upstream, oldUpstream, branchName+"~0")
	if err != nil {
		if r.IsRebaseInProgress(ctx) {
			return RebaseConflict, nil
		}
		// Abort rebase if it failed for other reasons
		_, _ = r.runGitCommandWithContextInternal(ctx, "rebase", "--abort")

		return RebaseConflict, err
	}

	// Since we rebased in detached HEAD, we must manually update the branch ref
	newRev, err := r.GetCurrentRevision(ctx)
	if err != nil {
		return RebaseConflict, fmt.Errorf("failed to get revision after rebase: %w", err)
	}

	if err := r.UpdateBranchRef(ctx, branchName, newRev); err != nil {
		return RebaseConflict, fmt.Errorf("failed to update branch ref %s: %w", branchName, err)
	}

	return RebaseDone, nil
}

func (r *runner) RebaseContinueNoEdit(ctx context.Context) (RebaseResult, error) {
	_, err := r.RunGitCommandWithEnv(ctx, []string{"GIT_EDITOR=true"}, "rebase", "--continue")
	if err != nil {
		if r.IsRebaseInProgress(ctx) {
			return RebaseConflict, nil
		}
		return RebaseConflict, err
	}
	return RebaseDone, nil
}

func (r *runner) RebaseContinue(ctx context.Context) (RebaseResult, error) {
	_, err := r.RunGitCommandWithEnv(ctx, []string{"GIT_EDITOR=true"}, "rebase", "--continue")
	if err != nil {
		if r.IsRebaseInProgress(ctx) {
			return RebaseConflict, nil
		}
		return RebaseConflict, fmt.Errorf("rebase continue failed: %w", err)
	}

	return RebaseDone, nil
}

func (r *runner) RebaseAbort(ctx context.Context) error {
	_, err := r.runGitCommandWithContextInternal(ctx, "rebase", "--abort")
	if err != nil {
		return fmt.Errorf("rebase abort failed: %w", err)
	}
	return nil
}

func (r *runner) InteractiveRebase(_ context.Context, onto string) error {
	return r.RunGitCommandInteractive("rebase", "-i", onto)
}

func (r *runner) GetRebaseHead() (string, error) {
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
		output, err := r.runGitCommandInternal("rev-parse", "--verify", refName)
		if err == nil && output != "" {
			return strings.TrimSpace(output), nil
		}
	}

	return "", fmt.Errorf("rebase head not found")
}

func (r *runner) IsRebaseInProgress(ctx context.Context) bool {
	output, err := r.runGitCommandWithContextInternal(ctx, "rev-parse", "--git-dir")
	if err != nil {
		return false
	}

	gitDir := strings.TrimSpace(output)
	if _, err := os.Stat(gitDir + "/rebase-merge"); err == nil {
		return true
	}
	if _, err := os.Stat(gitDir + "/rebase-apply"); err == nil {
		return true
	}
	return false
}

func (r *runner) CheckRebaseInProgress(ctx context.Context) error {
	if r.IsRebaseInProgress(ctx) {
		return fmt.Errorf("a rebase is already in progress. Please finish or abort it first")
	}
	return nil
}
