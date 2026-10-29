package actions

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"

	"github.com/getstackit/stackit/internal/engine"
)

const (
	restackWorktreeHoldReasonRebase               = "rebase in progress"
	restackWorktreeHoldReasonUncommittedChanges   = "uncommitted changes"
	restackWorktreeHoldReasonUntrackedCollision   = "an untracked file would be overwritten"
	restackWorktreeHoldReasonUntrackedCheckFailed = "untracked files could not be checked against the incoming commit"
)

// RestackWorktreeHoldScope describes the work that a hold prevents.
type RestackWorktreeHoldScope string

const (
	// RestackWorktreeHoldStack prevents restacking every branch in a stack.
	RestackWorktreeHoldStack RestackWorktreeHoldScope = "stack"
	// RestackWorktreeHoldBranch prevents restacking only one branch.
	RestackWorktreeHoldBranch RestackWorktreeHoldScope = "branch"
)

// RestackWorktreeHold describes a worktree that prevents a branch or stack
// from being safely restacked. It is read-only diagnostic data; callers must
// never move refs or reset worktrees in response to it.
type RestackWorktreeHold struct {
	Scope        RestackWorktreeHoldScope
	Branch       string
	StackRoot    string
	WorktreePath engine.WorktreePath
	Reason       string
}

// RestackWorktreeHoldReport contains the holds that affect a restack and
// warnings from worktree inspection that could not produce a specific hold.
type RestackWorktreeHoldReport struct {
	Holds    []RestackWorktreeHold
	Warnings []string
}

// CollectRestackWorktreeHolds inspects every physical checkout and reports
// the branches and stacks a restack must leave alone. sync and restack both
// read this one report, so they hold back exactly the same work.
func CollectRestackWorktreeHolds(ctx context.Context, eng engine.Engine) RestackWorktreeHoldReport {
	report := RestackWorktreeHoldReport{}

	stackHolds, err := ManagedWorktreeHolds(ctx, eng)
	if err != nil {
		report.Warnings = append(report.Warnings, fmt.Sprintf("Could not inspect managed worktrees; holding no known stacks: %v", err))
	}
	report.Holds = append(report.Holds, stackHolds...)

	addBranchHold := func(branch string, path engine.WorktreePath, reason string) {
		report.Holds = append(report.Holds, RestackWorktreeHold{
			Scope:        RestackWorktreeHoldBranch,
			Branch:       branch,
			StackRoot:    eng.GetStackRootForBranch(eng.GetBranch(branch)),
			WorktreePath: path,
			Reason:       reason,
		})
	}
	trunk := eng.Trunk().Name()
	if worktrees, err := eng.ListWorktrees(ctx); err == nil {
		for _, wt := range worktrees {
			if wt.Branch == "" || report.blocksStack(wt.Branch) || wt.Branch == trunk {
				continue
			}
			if reason := branchWorktreeHoldReason(ctx, eng, wt.Branch, wt.Path); reason != "" {
				addBranchHold(wt.Branch, wt.Path, reason)
			}
		}
	} else {
		report.Warnings = append(report.Warnings, fmt.Sprintf("Could not inspect Git worktrees; restack safety checks were incomplete: %v", err))
	}

	return report
}

// ManagedWorktreeSource is the engine surface ManagedWorktreeHolds needs.
type ManagedWorktreeSource interface {
	ManagedWorktreeInspector
	ListManagedWorktrees(ctx context.Context) ([]engine.WorktreeInfo, error)
}

// ManagedWorktreeHolds returns a stack-scoped hold for every managed worktree
// whose checkout cannot be safely reset, in registry order. sync's --dry-run
// preview calls it directly so the preview cannot promise work the run skips.
func ManagedWorktreeHolds(ctx context.Context, eng ManagedWorktreeSource) ([]RestackWorktreeHold, error) {
	managed, err := eng.ListManagedWorktrees(ctx)
	if err != nil {
		return nil, err
	}
	var holds []RestackWorktreeHold
	for _, wt := range managed {
		if reason := ManagedWorktreeHoldReason(ctx, eng, wt.Path); reason != "" {
			holds = append(holds, RestackWorktreeHold{
				Scope:        RestackWorktreeHoldStack,
				Branch:       wt.AnchorBranch,
				StackRoot:    wt.AnchorBranch,
				WorktreePath: wt.Path,
				Reason:       reason,
			})
		}
	}
	return holds, nil
}

// Messages returns one human-readable line per hold and per inspection
// warning, for adapters to print before the restack runs.
func (r RestackWorktreeHoldReport) Messages() []string {
	messages := make([]string, 0, len(r.Holds)+len(r.Warnings))
	for _, hold := range r.Holds {
		messages = append(messages, hold.Warning())
	}
	return append(messages, r.Warnings...)
}

// Filter drops every branch a hold covers, preserving order.
func (r RestackWorktreeHoldReport) Filter(eng engine.Engine, branches engine.Branches) engine.Branches {
	if len(r.Holds) == 0 {
		return branches
	}
	return branches.Filter(func(branch engine.Branch) bool {
		return !r.blocks(branch.Name(), eng.GetStackRootForBranch(branch))
	})
}

func (r RestackWorktreeHoldReport) blocks(branch, stackRoot string) bool {
	for _, hold := range r.Holds {
		if hold.Scope == RestackWorktreeHoldStack && hold.StackRoot == stackRoot {
			return true
		}
		if hold.Scope == RestackWorktreeHoldBranch && hold.Branch == branch {
			return true
		}
	}
	return false
}

func (r RestackWorktreeHoldReport) blocksStack(stackRoot string) bool {
	return slices.ContainsFunc(r.Holds, func(hold RestackWorktreeHold) bool {
		return hold.Scope == RestackWorktreeHoldStack && hold.StackRoot == stackRoot
	})
}

// Warning renders the hold as the line printed before a restack runs.
func (h RestackWorktreeHold) Warning() string {
	if h.Scope == RestackWorktreeHoldStack {
		switch h.Reason {
		case restackWorktreeHoldReasonRebase:
			return fmt.Sprintf("Holding stack rooted at %s (worktree %s has a rebase in progress)", h.StackRoot, h.WorktreePath)
		case restackWorktreeHoldReasonUncommittedChanges:
			return fmt.Sprintf("Skipping stack rooted at %s (worktree %s has uncommitted changes)", h.StackRoot, h.WorktreePath)
		default:
			return fmt.Sprintf("Holding stack rooted at %s because worktree %s %s", h.StackRoot, h.WorktreePath, h.Reason)
		}
	}

	switch h.Reason {
	case restackWorktreeHoldReasonRebase:
		return fmt.Sprintf("Holding %s (worktree %s has a rebase in progress)", h.Branch, h.WorktreePath)
	case restackWorktreeHoldReasonUncommittedChanges:
		return fmt.Sprintf("Skipping %s (worktree %s has uncommitted changes; commit or stash them, then restack again)", h.Branch, h.WorktreePath)
	case restackWorktreeHoldReasonUntrackedCollision:
		return fmt.Sprintf("Skipping %s (an untracked file in worktree %s would be overwritten by the incoming commit; move or commit it, then restack again)", h.Branch, h.WorktreePath)
	case restackWorktreeHoldReasonUntrackedCheckFailed:
		return fmt.Sprintf("Holding %s because untracked files in worktree %s could not be checked against the incoming commit", h.Branch, h.WorktreePath)
	default:
		return fmt.Sprintf("Holding %s because worktree %s %s", h.Branch, h.WorktreePath, h.Reason)
	}
}

// ManagedWorktreeInspector is the narrow dependency ManagedWorktreeHoldReason
// needs.
type ManagedWorktreeInspector interface {
	WorktreeRebaseInProgress(ctx context.Context, worktreePath engine.WorktreePath) bool
	WorktreeHasUncommittedChanges(ctx context.Context, worktreePath engine.WorktreePath) (bool, error)
}

// ManagedWorktreeHoldReason returns why a managed worktree's stack must be left
// alone this pass, or "" when it is safe to touch.
//
// A failed inspection counts as unsafe. The probe fails when git cannot read
// the worktree — an index.lock held by another process, unreadable permissions
// — and reading that as clean would move refs under a checkout whose contents
// are unknown, which is exactly what the hold exists to prevent.
//
// A missing directory is the one exception: there is no working tree left to
// protect, and holding it would block sync's orphan cleanup, which exists to
// retire its registration.
func ManagedWorktreeHoldReason(ctx context.Context, eng ManagedWorktreeInspector, path engine.WorktreePath) string {
	if _, err := os.Stat(path.String()); errors.Is(err, os.ErrNotExist) {
		return ""
	}
	if eng.WorktreeRebaseInProgress(ctx, path) {
		return restackWorktreeHoldReasonRebase
	}
	hasChanges, err := eng.WorktreeHasUncommittedChanges(ctx, path)
	if err != nil {
		return fmt.Sprintf("could not be inspected: %v", err)
	}
	if hasChanges {
		return restackWorktreeHoldReasonUncommittedChanges
	}
	return ""
}

func branchWorktreeHoldReason(ctx context.Context, eng engine.Engine, branch string, path engine.WorktreePath) string {
	if eng.WorktreeRebaseInProgress(ctx, path) {
		return restackWorktreeHoldReasonRebase
	}
	hasChanges, err := eng.WorktreeHasTrackedChanges(ctx, path)
	if err != nil {
		return fmt.Sprintf("could not be inspected: %v", err)
	}
	if hasChanges {
		return restackWorktreeHoldReasonUncommittedChanges
	}
	if collides, known := eng.UntrackedCollision(ctx, branch, path); collides || !known {
		if !known {
			return restackWorktreeHoldReasonUntrackedCheckFailed
		}
		return restackWorktreeHoldReasonUntrackedCollision
	}
	return ""
}
