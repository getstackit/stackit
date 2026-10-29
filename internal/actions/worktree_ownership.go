package actions

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"

	"github.com/getstackit/stackit/internal/engine"
)

// Here is where a command runs: the main repository, or a stackit-managed
// worktree.
type Here struct {
	InManagedWorktree bool
	// Worktree describes the managed worktree when InManagedWorktree is set.
	Worktree *engine.WorktreeInfo
}

// OwnershipEngine is the engine surface the ownership guard reads.
type OwnershipEngine interface {
	engine.BranchLookup
	WorktreeOwnershipByStackRoot(ctx context.Context) (map[string]engine.WorktreeOwnership, error)
	GetStackRootForBranch(branch engine.Branch) string
}

// EnsureCanModify refuses to mutate a stack from anywhere other than its
// managed worktree. The ownership relation is derived from the branch's stack
// root, while the invocation location comes from here.
//
// Branch-level mutation checks deliberately do not live in engine.Branch:
// Branch has no command context, and the same branch can be valid in one
// worktree and invalid in another.
func EnsureCanModify(ctx context.Context, eng OwnershipEngine, here Here, branches ...engine.Branch) error {
	// Resolve ownership for every branch from a single listing instead of one
	// OwningWorktree call (two git reads each) per branch.
	owners, err := LoadWorktreeOwners(ctx, eng)
	if err != nil {
		return fmt.Errorf("cannot list managed worktrees: %w", err)
	}

	seen := make(map[string]struct{}, len(branches))
	for _, branch := range branches {
		branchName := branch.Name()
		if branchName == "" {
			continue
		}
		if _, ok := seen[branchName]; ok {
			continue
		}
		seen[branchName] = struct{}{}

		owner, err := owners.Owner(branch)
		if err != nil {
			return fmt.Errorf("cannot determine worktree ownership for branch %s: %w", branchName, err)
		}

		if owner == nil {
			if here.InManagedWorktree {
				return fmt.Errorf("branch %s belongs to the main repository stack; run this command from %s", branchName, here.Worktree.MainRepoDir)
			}
			continue
		}

		if here.InManagedWorktree && here.Worktree != nil && here.Worktree.AnchorBranch == owner.AnchorBranch {
			continue
		}

		// "cd there" is only useful advice if "there" still exists. A worktree
		// whose directory was deleted leaves the branch owned by a path the
		// user cannot go to, and the way out is detaching the registration —
		// not a cd that will fail.
		if _, statErr := os.Stat(owner.Path.String()); statErr != nil {
			if errors.Is(statErr, os.ErrNotExist) {
				return fmt.Errorf("branch %s belongs to worktree %s, whose directory no longer exists (%s); run 'stackit worktree detach %s' to release it, or 'stackit worktree list' to review",
					branchName, owner.DisplayName(), owner.Path, owner.DisplayName())
			}
			return fmt.Errorf("branch %s belongs to worktree %s, which cannot be inspected at %s: %w; 'stackit worktree list' shows the current state",
				branchName, owner.DisplayName(), owner.Path, statErr)
		}

		return fmt.Errorf("branch %s belongs to worktree %s; run this command from there: cd %s", branchName, owner.DisplayName(), owner.Path)
	}

	return nil
}

// WorktreeOwners answers OwningWorktree for many branches from one listing of
// worktree registrations.
type WorktreeOwners struct {
	eng         OwnershipEngine
	byStackRoot map[string]engine.WorktreeOwnership
}

// LoadWorktreeOwners reads every worktree registration once.
func LoadWorktreeOwners(ctx context.Context, eng OwnershipEngine) (WorktreeOwners, error) {
	byStackRoot, err := eng.WorktreeOwnershipByStackRoot(ctx)
	if err != nil {
		return WorktreeOwners{}, err
	}
	return WorktreeOwners{eng: eng, byStackRoot: byStackRoot}, nil
}

// Owner returns the managed worktree that owns branch's stack, or nil for an
// ordinary stack. A malformed registration is an error, never "unowned", so
// guards fail closed.
func (o WorktreeOwners) Owner(branch engine.Branch) (*engine.WorktreeInfo, error) {
	ownership, ok := o.byStackRoot[o.eng.GetStackRootForBranch(branch)]
	if !ok {
		return nil, nil
	}
	return ownership.Worktree, ownership.Err
}

// EnsureCanModifyNames is EnsureCanModify for branch names.
func EnsureCanModifyNames(ctx context.Context, eng OwnershipEngine, here Here, branchNames ...string) error {
	uniqueNames := slices.Compact(slices.Sorted(slices.Values(branchNames)))
	branches := make([]engine.Branch, 0, len(uniqueNames))
	for _, name := range uniqueNames {
		if name != "" {
			branches = append(branches, eng.GetBranch(name))
		}
	}
	return EnsureCanModify(ctx, eng, here, branches...)
}
