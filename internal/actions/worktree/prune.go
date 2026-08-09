package worktree

import (
	"fmt"

	"github.com/getstackit/stackit/internal/app"
)

// PruneOptions contains options for the prune action.
type PruneOptions struct {
	DryRun bool // If true, only show what would be pruned
}

// PruneResult contains the results of pruning worktrees.
type PruneResult struct {
	Pruned  []string       // Names of pruned worktrees
	Skipped []SkippedEntry // Worktrees that were skipped and why
}

// SkippedEntry represents a worktree that was skipped during pruning.
type SkippedEntry struct {
	Name   string
	Reason string
}

// PruneAction removes all empty worktrees.
func PruneAction(ctx *app.Context, opts PruneOptions) (*PruneResult, error) {
	listResult, err := ListAction(ctx, ListOptions{})
	if err != nil {
		return nil, err
	}

	result := &PruneResult{
		Pruned:  make([]string, 0),
		Skipped: make([]SkippedEntry, 0),
	}
	for _, wt := range listResult.Worktrees {
		name := wt.displayName()
		if reason := pruneRefusal(&wt); reason != "" {
			result.Skipped = append(result.Skipped, SkippedEntry{Name: name, Reason: reason})
			continue
		}
		if opts.DryRun {
			result.Pruned = append(result.Pruned, name)
			continue
		}
		if err := RemoveAction(ctx, RemoveOptions{
			Selector: WorktreeSelector(wt.AnchorBranch),
			Policy:   pruneRemovalPolicy(wt.Lifecycle),
		}); err != nil {
			result.Skipped = append(result.Skipped, SkippedEntry{Name: name, Reason: fmt.Sprintf("removal failed: %v", err)})
			continue
		}
		result.Pruned = append(result.Pruned, name)
	}
	return result, nil
}

// pruneRefusal returns why a worktree cannot be pruned, or "" when it can.
func pruneRefusal(wt *Entry) string {
	switch wt.Lifecycle.Remove {
	case LifecycleNeedsRepair:
		return wt.StatusMessage + "; " + repairHint(wt)
	case LifecycleCurrent:
		return "currently in this worktree"
	case LifecycleHasBranches:
		return fmt.Sprintf("has %d stacked branches; use 'stackit worktree detach %s' to remove the worktree while keeping them", wt.StackSize, wt.displayName())
	}
	if wt.Lifecycle.Exists() && wt.Lifecycle.IsDirty() {
		return "has uncommitted changes"
	}
	return ""
}

func pruneRemovalPolicy(lifecycle WorktreeLifecycle) RemovalPolicy {
	if lifecycle.Presence == WorktreeMissing {
		return RemovalDiscardChanges
	}
	return RemovalRespectChanges
}
