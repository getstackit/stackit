package worktree

import (
	"context"
	"fmt"
	"os"

	"github.com/getstackit/stackit/internal/engine"
)

// RemovalPolicy makes the destructive intent of worktree removal explicit.
type RemovalPolicy uint8

const (
	RemovalRespectChanges RemovalPolicy = iota
	RemovalDiscardChanges
)

// DiscardsChanges reports whether removal may discard uncommitted changes.
func (p RemovalPolicy) DiscardsChanges() bool {
	return p == RemovalDiscardChanges
}

// WorktreeRemover removes linked worktrees.
type WorktreeRemover interface {
	RemoveWorktree(ctx context.Context, path engine.WorktreePath) error
	ForceRemoveWorktree(ctx context.Context, path engine.WorktreePath) error
}

// RemovePath removes a linked worktree when its directory exists. A missing
// directory is not an error: callers can prune Git's stale administrative
// entry according to their own lifecycle policy.
func RemovePath(ctx context.Context, eng WorktreeRemover, path engine.WorktreePath, policy RemovalPolicy) (removed bool, err error) {
	if _, err := os.Stat(path.String()); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("inspect worktree path %s: %w", path, err)
	}

	if policy.DiscardsChanges() {
		if err := eng.ForceRemoveWorktree(ctx, path); err != nil {
			return false, err
		}
	} else if err := eng.RemoveWorktree(ctx, path); err != nil {
		return false, err
	}
	return true, nil
}
