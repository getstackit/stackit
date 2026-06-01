package engine

import (
	"context"
	"fmt"

	"github.com/getstackit/stackit/internal/git"
)

// PullTrunk pulls the trunk branch from remote
func (e *engineImpl) PullTrunk(ctx context.Context) (PullResult, error) {
	remote := e.git.GetRemote()
	e.mu.RLock()
	trunk := e.trunk
	e.mu.RUnlock()
	gitResult, err := e.git.PullBranch(ctx, remote, trunk)
	if err != nil {
		return PullConflict, err
	}

	return e.finishTrunkPull(gitResult)
}

// UpdateTrunkFromRemote updates trunk from the already-fetched remote-tracking
// branch. Callers that need to batch the network fetch with other refspecs can
// use FetchRemote first, then call this local update step.
func (e *engineImpl) UpdateTrunkFromRemote(ctx context.Context) (PullResult, error) {
	remote := e.git.GetRemote()
	e.mu.RLock()
	trunk := e.trunk
	e.mu.RUnlock()
	gitResult, err := e.git.UpdateBranchFromRemote(ctx, remote, trunk)
	if err != nil {
		return PullConflict, err
	}

	return e.finishTrunkPull(gitResult)
}

func (e *engineImpl) finishTrunkPull(gitResult git.PullResult) (PullResult, error) {
	// Convert git.PullResult to engine.PullResult
	var result PullResult
	switch gitResult {
	case git.PullDone:
		result = PullDone
	case git.PullUnneeded:
		result = PullUnneeded
	case git.PullConflict:
		result = PullConflict
	default:
		result = PullConflict
	}

	// Rebuild to refresh branch cache
	if err := e.rebuild(); err != nil {
		return result, fmt.Errorf("failed to rebuild after pull: %w", err)
	}

	return result, nil
}

// ResetTrunkToRemote resets trunk to match remote
func (e *engineImpl) ResetTrunkToRemote(ctx context.Context) error {
	remote := e.git.GetRemote()

	e.mu.RLock()
	trunk := e.trunk
	currentBranch := e.currentBranch
	e.mu.RUnlock()

	// Get remote SHA
	remoteSha, err := e.git.GetRemoteSha(remote, trunk)
	if err != nil {
		// Fallback: try to get it from ls-remote if the tracking branch is missing
		remoteShas, fetchErr := e.git.FetchRemoteShas(ctx, remote)
		if fetchErr == nil {
			if sha, ok := remoteShas[trunk]; ok {
				remoteSha = sha
				err = nil
			}
		}
	}

	if err != nil {
		return fmt.Errorf("failed to get remote SHA: %w", err)
	}

	// Checkout trunk
	trunkBranch := e.Trunk()
	if err := e.CheckoutBranch(ctx, trunkBranch); err != nil {
		return fmt.Errorf("failed to checkout trunk: %w", err)
	}

	// Hard reset to remote
	if err := e.git.HardReset(ctx, remoteSha); err != nil {
		// Try to switch back
		if currentBranch != "" {
			currentBranchObj := e.GetBranch(currentBranch)
			_ = e.CheckoutBranch(ctx, currentBranchObj)
		}
		return fmt.Errorf("failed to reset trunk: %w", err)
	}

	// Switch back to original branch
	if currentBranch != "" && currentBranch != trunk {
		currentBranchObj := e.GetBranch(currentBranch)
		if err := e.CheckoutBranch(ctx, currentBranchObj); err != nil {
			return fmt.Errorf("failed to switch back: %w", err)
		}
	}

	// Rebuild to refresh branch cache
	if err := e.rebuild(); err != nil {
		return fmt.Errorf("failed to rebuild after reset: %w", err)
	}

	return nil
}
