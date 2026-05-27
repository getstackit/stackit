package engine

import (
	"context"
	"fmt"

	"github.com/getstackit/stackit/internal/errors"
	"github.com/getstackit/stackit/internal/git"
)

// SquashCurrentBranch squashes all commits in the current branch into a single commit
func (e *engineImpl) SquashCurrentBranch(ctx context.Context, opts SquashOptions) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Get current branch
	branchName := e.currentBranch
	if branchName == "" {
		return errors.ErrNotOnBranch
	}

	// Check if branch is trunk (check directly since we hold the lock)
	if branchName == e.trunk {
		return fmt.Errorf("cannot squash trunk branch")
	}

	// Read metadata to get parent branch revision
	meta, err := e.readMetadata(branchName)
	if err != nil {
		return fmt.Errorf("failed to read metadata: %w", err)
	}

	if meta.GetParentBranchRevision() == nil {
		return fmt.Errorf("branch has no parent revision")
	}

	parentBranchRevision := *meta.GetParentBranchRevision()

	// Get current branch revision
	branch := e.GetBranch(branchName)
	branchRevision, err := branch.GetRevision()
	if err != nil {
		return fmt.Errorf("failed to get branch revision: %w", err)
	}

	// Get commit range SHAs from parent to current branch
	commitSHAs, err := e.git.GetCommitRangeSHAs(ctx, parentBranchRevision, branchRevision)
	if err != nil {
		return fmt.Errorf("failed to get commit range: %w", err)
	}

	// Check if there are commits to squash
	if len(commitSHAs) == 0 {
		return fmt.Errorf("no commits to squash")
	}

	// Get the last (oldest) commit SHA from the range
	// GetCommitRangeSHAs returns newest first (head...base)
	oldestCommitSHA := commitSHAs[len(commitSHAs)-1]

	// Soft reset to the oldest commit (keeps all changes staged)
	// This moves HEAD to the oldest commit, staging all changes from newer commits
	if err := e.git.SoftReset(ctx, oldestCommitSHA); err != nil {
		return fmt.Errorf("failed to soft reset: %w", err)
	}

	// Commit with amend flag to modify the oldest commit to include all changes
	// This is correct: we reset to the oldest commit, then amend it to include all subsequent changes
	// Only pass noEdit and message, let git handle editor by default
	commitOpts := git.CommitOptions{
		Amend:    true,
		Message:  opts.Message,
		NoEdit:   opts.NoEdit,
		NoVerify: opts.NoVerify,
		// Don't set Edit - git will open editor by default if no message and no noEdit
	}

	if err := e.git.CommitWithOptions(commitOpts); err != nil {
		// Try to rollback on error
		if rollbackErr := e.git.SoftReset(ctx, branchRevision); rollbackErr != nil {
			// Log rollback error but return original error
			return fmt.Errorf("failed to commit and failed to rollback: commit error: %w, rollback error: %w", err, rollbackErr)
		}
		return fmt.Errorf("failed to commit: %w", err)
	}

	// Rebuild to refresh cache (parent/children relationships remain the same)
	// Don't refresh currentBranch - we're still on the same branch
	if err := e.rebuildInternal(false); err != nil {
		return fmt.Errorf("failed to rebuild after squash: %w", err)
	}

	return nil
}
