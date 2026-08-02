package actions

import (
	"fmt"
	"slices"

	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/engine"
)

// EnsureCanModifyHere refuses to mutate a stack from anywhere other than its
// managed worktree. The ownership relation is derived from the branch's stack
// root, while the invocation location comes from the command context.
//
// Branch-level mutation checks deliberately do not live in engine.Branch:
// Branch has no command context, and the same branch can be valid in one
// worktree and invalid in another.
func EnsureCanModifyHere(ctx *app.Context, branches ...engine.Branch) error {
	seen := make(map[string]struct{}, len(branches))
	for _, branch := range branches {
		branchName := branch.GetName()
		if branchName == "" {
			continue
		}
		if _, ok := seen[branchName]; ok {
			continue
		}
		seen[branchName] = struct{}{}

		owner, err := ctx.Engine.OwningWorktree(branch)
		if err != nil {
			return fmt.Errorf("cannot determine worktree ownership for branch %s: %w", branchName, err)
		}

		if owner == nil {
			if ctx.InManagedWorktree {
				return fmt.Errorf("branch %s belongs to the main repository stack; run this command from %s", branchName, ctx.WorktreeInfo.MainRepoDir)
			}
			continue
		}

		if ctx.InManagedWorktree && ctx.WorktreeInfo != nil && ctx.WorktreeInfo.AnchorBranch == owner.AnchorBranch {
			continue
		}

		return fmt.Errorf("branch %s belongs to worktree %s; run this command from there: cd %s", branchName, worktreeDisplayName(owner), owner.Path)
	}

	return nil
}

func worktreeDisplayName(worktree *engine.WorktreeInfo) string {
	if worktree.Name != "" {
		return worktree.Name
	}
	return worktree.AnchorBranch
}

// EnsureCanModifyNamesHere is the convenient form for operations that resolve
// their targets as branch names before constructing a plan.
func EnsureCanModifyNamesHere(ctx *app.Context, branchNames ...string) error {
	uniqueNames := slices.Compact(slices.Sorted(slices.Values(branchNames)))
	branches := make([]engine.Branch, 0, len(uniqueNames))
	for _, name := range uniqueNames {
		if name != "" {
			branches = append(branches, ctx.Engine.GetBranch(name))
		}
	}
	return EnsureCanModifyHere(ctx, branches...)
}
