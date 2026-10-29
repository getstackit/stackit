package worktree

import (
	"context"
	"errors"
	"fmt"

	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/output"
	worktreeutil "github.com/getstackit/stackit/internal/worktree"
)

// DetachOptions contains options for the detach action.
type DetachOptions struct {
	Selector WorktreeSelector // Worktree name or anchor branch
	Policy   RemovalPolicy    // Whether detach may discard uncommitted changes
}

// DetachAction removes a worktree while preserving all branches.
func DetachAction(ctx context.Context, deps Deps, opts DetachOptions) error {
	entry, err := getWorktreeEntry(ctx, deps, opts.Selector)
	if err != nil {
		return err
	}
	if refusal := entry.detachRefusal(opts.Policy); refusal != "" {
		return errors.New(refusal)
	}

	snapshot, err := snapshotWorktree(ctx, deps, entry)
	if err != nil {
		return err
	}
	if entry.Lifecycle.NeedsRepair() {
		if entry.Lifecycle.Exists() {
			if _, err := worktreeutil.RemovePath(ctx, deps.Engine, entry.Path, opts.Policy); err != nil {
				return fmt.Errorf("failed to remove worktree at %s: %w", entry.Path, err)
			}
		} else if err := deps.Engine.PruneWorktrees(ctx); err != nil {
			return fmt.Errorf("failed to prune stale worktree entries: %w", err)
		}
		if err := deps.Engine.UnregisterWorktree(ctx, entry.AnchorBranch); err != nil {
			return fmt.Errorf("failed to unregister invalid worktree: %w", err)
		}
		deps.Out.Success("Detached invalid worktree %s", output.BranchName(entry.displayName()))
		return nil
	}

	mutation := worktreeMutation{snapshot: snapshot}
	mutation.pathRemoved, err = removeWorktreeOrPruneMissing(ctx, deps, snapshot.Info.Path, opts.Policy)
	if err != nil {
		if opts.Policy.DiscardsChanges() {
			return fmt.Errorf("failed to force remove worktree at %s: %w", snapshot.Info.Path, err)
		}
		return fmt.Errorf("failed to remove worktree at %s: %w (use --force to discard uncommitted changes)", snapshot.Info.Path, err)
	}
	if snapshot.AnchorExists && len(snapshot.ChildNames) > 0 {
		if err := deps.Engine.ReparentBranchesToParents(ctx, engine.MovesTo(snapshot.ChildNames, snapshot.AnchorParent), engine.ReparentOpts{}); err != nil {
			return mutation.rollback(ctx, deps, fmt.Errorf("failed to reparent children to %s: %w", snapshot.AnchorParent, err), RestoreChildren)
		}
		mutation.reparented = true
	}
	if snapshot.AnchorExists {
		if err := deps.Engine.DeleteBranch(ctx, deps.Engine.GetBranch(snapshot.Info.AnchorBranch)); err != nil {
			return mutation.rollback(ctx, deps, fmt.Errorf("failed to delete anchor branch %s: %w", snapshot.Info.AnchorBranch, err), RestoreChildren)
		}
		mutation.anchorDeleted = true
		deps.Out.Debug("Deleted anchor branch %s", snapshot.Info.AnchorBranch)
	}
	if err := deps.Engine.UnregisterWorktree(ctx, snapshot.Info.AnchorBranch); err != nil {
		return mutation.rollback(ctx, deps, fmt.Errorf("failed to unregister worktree: %w", err), RestoreChildren)
	}
	mutation.unregistered = true
	deps.Out.Success("Detached worktree %s", output.BranchName(snapshot.Info.Name.String()))
	return nil
}
