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
	// Worktree sessions and similar ephemeral setups intentionally have no
	// remote configured; UpdateBranchFromRemote already tolerates that via
	// GetRemoteSha's error handling. Skip the fetch attempt entirely in that
	// case rather than surfacing "remote not configured" as a pull failure.
	if !r.remoteConfigured(ctx, remote) {
		return r.UpdateBranchFromRemote(ctx, remote, branchName)
	}

	// Fetch with explicit refspec to update the remote-tracking branch.
	// This ensures refs/remotes/<remote>/<branch> is actually updated.
	refspec := BranchFetchRefspec(remote, branchName)
	if err := r.fetchRemoteRefSpecs(ctx, remote, []string{refspec}); err != nil {
		return PullConflict, fmt.Errorf("failed to fetch %s from %s: %w", branchName, remote, err)
	}

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
		if err != nil {
			currentBranch = ""
		}

		// A ref update is global, but the checked-out worktree's index and
		// working directory are local. Never move a checked-out branch unless
		// its holder is known clean and can be reset immediately afterwards.
		// Importantly, an inspection failure is unsafe too: it must not silently
		// turn into a ref move that strands an unknown worktree on old content.
		worktrees, err := r.ListWorktrees(ctx)
		if err != nil {
			return PullConflict, fmt.Errorf("refusing to update %s: cannot inspect worktrees: %w", branchName, err)
		}
		worktreePath := worktrees.PathForBranch(branchName)
		if worktreePath != "" {
			hasChanges, statusErr := r.WorktreeHasTrackedChanges(ctx, worktreePath)
			if statusErr != nil {
				return PullConflict, fmt.Errorf("refusing to update %s: cannot inspect worktree %s: %w", branchName, worktreePath, statusErr)
			}
			if hasChanges {
				return PullConflict, fmt.Errorf("refusing to update %s: worktree %s has uncommitted tracked changes", branchName, worktreePath)
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
		}

		// If this branch is checked out in another clean worktree, reset it to
		// match the ref we just moved. The clean check above makes this safe.
		if worktreePath != "" && currentBranch != branchName {
			if err := r.ResetWorktreeWorkingDir(ctx, worktreePath); err != nil {
				return PullConflict, fmt.Errorf("updated %s but failed to reset clean worktree %s: %w", branchName, worktreePath, err)
			}
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
