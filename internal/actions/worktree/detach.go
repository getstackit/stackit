package worktree

import (
	"errors"
	"fmt"

	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/output"
	worktreeutil "github.com/getstackit/stackit/internal/worktree"
)

// DetachOptions contains options for the detach action.
type DetachOptions struct {
	Selector WorktreeSelector // Worktree name or anchor branch
	Policy   RemovalPolicy    // Whether detach may discard uncommitted changes
}

// DetachAction removes a worktree while preserving all branches.
func DetachAction(ctx *app.Context, opts DetachOptions) error {
	entry, err := getWorktreeEntry(ctx, opts.Selector)
	if err != nil {
		return err
	}
	if refusal := entry.detachRefusal(opts.Policy); refusal != "" {
		return errors.New(refusal)
	}

	snapshot, err := snapshotWorktree(ctx, entry)
	if err != nil {
		return err
	}
	if entry.Lifecycle.NeedsRepair() {
		if entry.Lifecycle.Exists() {
			if _, err := worktreeutil.RemovePath(ctx.Context, ctx.Engine, entry.Path.String(), opts.Policy); err != nil {
				return fmt.Errorf("failed to remove worktree at %s: %w", entry.Path, err)
			}
		} else if err := ctx.Engine.PruneWorktrees(ctx.Context); err != nil {
			return fmt.Errorf("failed to prune stale worktree entries: %w", err)
		}
		if err := ctx.Engine.UnregisterWorktree(ctx.Context, entry.AnchorBranch); err != nil {
			return fmt.Errorf("failed to unregister invalid worktree: %w", err)
		}
		ctx.Output.Success("Detached invalid worktree %s", output.BranchName(entry.displayName()))
		return nil
	}

	mutation := worktreeMutation{snapshot: snapshot}
	mutation.pathRemoved, err = removeWorktreeOrPruneMissing(ctx, snapshot.Info.Path.String(), opts.Policy)
	if err != nil {
		if opts.Policy.DiscardsChanges() {
			return fmt.Errorf("failed to force remove worktree at %s: %w", snapshot.Info.Path, err)
		}
		return fmt.Errorf("failed to remove worktree at %s: %w (use --force to discard uncommitted changes)", snapshot.Info.Path, err)
	}
	if snapshot.AnchorExists && len(snapshot.ChildNames) > 0 {
		if err := ctx.Engine.ReparentBranches(ctx.Context, snapshot.ChildNames, ctx.Engine.GetBranch(snapshot.AnchorParent)); err != nil {
			return mutation.rollback(ctx, fmt.Errorf("failed to reparent children to %s: %w", snapshot.AnchorParent, err), true)
		}
		mutation.reparented = true
	}
	if snapshot.AnchorExists {
		if err := ctx.Engine.DeleteBranch(ctx.Context, ctx.Engine.GetBranch(snapshot.Info.AnchorBranch)); err != nil {
			return mutation.rollback(ctx, fmt.Errorf("failed to delete anchor branch %s: %w", snapshot.Info.AnchorBranch, err), true)
		}
		mutation.anchorDeleted = true
		ctx.Output.Debug("Deleted anchor branch %s", snapshot.Info.AnchorBranch)
	}
	if err := ctx.Engine.UnregisterWorktree(ctx.Context, snapshot.Info.AnchorBranch); err != nil {
		return mutation.rollback(ctx, fmt.Errorf("failed to unregister worktree: %w", err), true)
	}
	mutation.unregistered = true
	ctx.Output.Success("Detached worktree %s", output.BranchName(snapshot.Info.Name.String()))
	return nil
}
