package git

import (
	"context"
	"fmt"
)

// PullResult represents the result of a pull operation
type PullResult int

const (
	// PullDone indicates the pull was successful
	PullDone PullResult = iota
	// PullUnneeded indicates no pull was needed
	PullUnneeded
	// PullConflict indicates a conflict occurred during pull
	PullConflict
)

func (r *runner) PullBranch(ctx context.Context, remote, branchName string) (PullResult, error) {
	// Save current branch/detached HEAD
	currentBranch, err := r.GetCurrentBranch()
	var currentRev string
	if err != nil {
		currentBranch = ""
		currentRev, _ = r.GetCurrentRevision(ctx)
	}

	// Get the SHA of the local branch
	oldRev, err := r.GetRevision(branchName)
	if err != nil {
		return PullConflict, fmt.Errorf("failed to get local revision for %s: %w", branchName, err)
	}

	// Fetch with explicit refspec to update the remote-tracking branch.
	// This ensures refs/remotes/origin/<branch> is actually updated.
	refspec := fmt.Sprintf("refs/heads/%s:refs/remotes/%s/%s", branchName, remote, branchName)
	_ = r.fetchRemoteRefSpecs(ctx, remote, []string{refspec})

	// Get the SHA of the remote branch
	remoteRev, err := r.GetRemoteSha(remote, branchName)
	if err != nil {
		// If we can't get remote rev, we can't pull, but it might just be because there's no remote
		return PullUnneeded, nil //nolint:nilerr
	}

	if oldRev == remoteRev {
		return PullUnneeded, nil
	}

	// Check if it's a fast-forward (remote is ahead of local)
	isRemoteAhead, err := r.IsAncestor(ctx, oldRev, remoteRev)
	if err == nil && isRemoteAhead {
		// Before updating the ref, check if this branch is checked out in another worktree.
		// update-ref is global and will affect all worktrees, so we need to sync any
		// worktree that has this branch checked out to avoid corrupting its index/working tree.
		//
		// IMPORTANT: We must check for uncommitted changes BEFORE the update-ref,
		// because after update-ref the worktree will appear to have inverse changes
		// (old content vs new HEAD), which would cause the check to fail.
		var otherWorktreePath string
		var otherWorktreeClean bool
		if currentBranch != branchName {
			otherWorktreePath, _ = r.GetWorktreePathForBranch(ctx, branchName)
			if otherWorktreePath != "" {
				hasChanges, err := r.WorktreeHasUncommittedChanges(ctx, otherWorktreePath)
				otherWorktreeClean = err == nil && !hasChanges
			}
		}

		// Update the local branch reference to the remote commit (fast-forward)
		if err := r.UpdateBranchRef(ctx, branchName, remoteRev); err != nil {
			return PullConflict, fmt.Errorf("failed to update local branch %s to %s: %w", branchName, remoteRev, err)
		}

		// If we are currently ON this branch in this worktree, we need to sync
		// the index and working tree with the new HEAD. After update-ref, HEAD
		// (via symbolic ref) already points to the new commit, but the index
		// still has old content. Using git checkout doesn't reliably update
		// the index when we're "already on" the branch.
		// Use hard reset to ensure index and working tree match the new HEAD.
		// NOTE: st sync checks for uncommitted changes before calling this,
		// so hard reset is safe here.
		if currentBranch == branchName {
			_ = r.HardReset(ctx, "HEAD")
		} else if currentRev != "" {
			_ = r.CheckoutDetached(ctx, currentRev)
		}

		// If this branch is checked out in another worktree that was clean before
		// the update-ref, we need to reset that worktree to match the new HEAD.
		// Otherwise the worktree's index/working tree will appear as inverse changes.
		if otherWorktreePath != "" && otherWorktreeClean {
			_ = r.ResetWorktreeWorkingDir(ctx, otherWorktreePath)
		}

		return PullDone, nil
	}

	// Check if local is already ahead of remote
	isLocalAhead, _ := r.IsAncestor(ctx, remoteRev, oldRev)
	if isLocalAhead {
		return PullUnneeded, nil
	}

	// Otherwise they have diverged
	return PullConflict, nil
}

func (r *runner) Fetch(ctx context.Context, remote, branch string) error {
	refspec := fmt.Sprintf("refs/heads/%s:refs/remotes/%s/%s", branch, remote, branch)
	if err := r.fetchRemoteRefSpecs(ctx, remote, []string{refspec}); err != nil {
		return fmt.Errorf("failed to fetch %s from %s: %w", branch, remote, err)
	}
	return nil
}

func (r *runner) FetchRefSpecs(ctx context.Context, remote string, refspecs []string) error {
	return r.fetchRemoteRefSpecs(ctx, remote, refspecs)
}
