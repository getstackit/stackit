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

// BranchFetchRefspec builds the refspec used to fetch a remote branch into its
// remote-tracking ref. The leading '+' forces the update so a force-pushed
// remote branch (whose new tip is not a descendant of the previously fetched
// tip) still updates refs/remotes/<remote>/<branch>, instead of failing the
// fetch as a non-fast-forward update. This mirrors the '+' in Git's default
// "+refs/heads/*:refs/remotes/origin/*" fetch refspec.
func BranchFetchRefspec(remote, branch string) string {
	return fmt.Sprintf("+refs/heads/%s:refs/remotes/%s/%s", branch, remote, branch)
}

func (r *runner) PullBranch(ctx context.Context, remote, branchName string) (PullResult, error) {
	// Fetch with explicit refspec to update the remote-tracking branch.
	// This ensures refs/remotes/<remote>/<branch> is actually updated.
	refspec := BranchFetchRefspec(remote, branchName)
	_ = r.fetchRemoteRefSpecs(ctx, remote, []string{refspec})

	return r.UpdateBranchFromRemote(ctx, remote, branchName)
}

func (r *runner) UpdateBranchFromRemote(ctx context.Context, remote, branchName string) (PullResult, error) {
	// Get the SHA of the local branch
	oldRev, err := r.GetRevision(branchName)
	if err != nil {
		return PullConflict, fmt.Errorf("failed to get local revision for %s: %w", branchName, err)
	}

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
		// Save current branch/detached HEAD immediately before mutating refs.
		currentBranch, err := r.GetCurrentBranch()
		var currentRev string
		if err != nil {
			currentBranch = ""
			currentRev, _ = r.GetCurrentRevision(ctx)
		}

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
	refspec := BranchFetchRefspec(remote, branch)
	if err := r.fetchRemoteRefSpecs(ctx, remote, []string{refspec}); err != nil {
		return fmt.Errorf("failed to fetch %s from %s: %w", branch, remote, err)
	}
	return nil
}

func (r *runner) FetchRefSpecs(ctx context.Context, remote string, refspecs []string) error {
	return r.fetchRemoteRefSpecs(ctx, remote, refspecs)
}
