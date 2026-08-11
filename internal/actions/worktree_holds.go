package actions

import (
	"fmt"

	"github.com/getstackit/stackit/internal/app"
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
	WorktreePath string
	Reason       string
}

// RestackWorktreeHoldReport contains the holds that affect a restack and
// warnings from worktree inspection that could not produce a specific hold.
type RestackWorktreeHoldReport struct {
	Holds    []RestackWorktreeHold
	Warnings []string
}

// RestackWorktreeHolds reports the exact worktree conditions that would make
// restack hold back work. It shares the calculation used by PlanRestack, so
// read-only consumers cannot drift from the safety decision.
func RestackWorktreeHolds(ctx *app.Context) RestackWorktreeHoldReport {
	return collectRestackWorktreeHolds(ctx)
}

func collectRestackWorktreeHolds(ctx *app.Context) RestackWorktreeHoldReport {
	eng := ctx.Engine
	report := RestackWorktreeHoldReport{}

	addAnchorHold := func(anchor, path, reason string) {
		report.Holds = append(report.Holds, RestackWorktreeHold{
			Scope:        RestackWorktreeHoldStack,
			Branch:       anchor,
			StackRoot:    anchor,
			WorktreePath: path,
			Reason:       reason,
		})
	}
	if managed, err := eng.ListManagedWorktrees(); err == nil {
		for _, wt := range managed {
			path := wt.Path.String()
			if reason := managedWorktreeHoldReason(ctx, path); reason != "" {
				addAnchorHold(wt.AnchorBranch, path, reason)
			}
		}
	} else {
		report.Warnings = append(report.Warnings, fmt.Sprintf("Could not inspect managed worktrees; holding no known stacks: %v", err))
	}

	addBranchHold := func(branch, path, reason string) {
		report.Holds = append(report.Holds, RestackWorktreeHold{
			Scope:        RestackWorktreeHoldBranch,
			Branch:       branch,
			StackRoot:    eng.GetStackRootForBranch(eng.GetBranch(branch)),
			WorktreePath: path,
			Reason:       reason,
		})
	}
	trunk := eng.Trunk().GetName()
	if worktrees, err := eng.ListWorktrees(ctx.Context); err == nil {
		for _, wt := range worktrees {
			if wt.Branch == "" || report.blocksStack(wt.Branch) || wt.Branch == trunk {
				continue
			}
			if reason := branchWorktreeHoldReason(ctx, wt.Branch, wt.Path); reason != "" {
				addBranchHold(wt.Branch, wt.Path, reason)
			}
		}
	} else {
		report.Warnings = append(report.Warnings, fmt.Sprintf("Could not inspect Git worktrees; restack safety checks were incomplete: %v", err))
	}

	return report
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
	for _, hold := range r.Holds {
		if hold.Scope == RestackWorktreeHoldStack && hold.StackRoot == stackRoot {
			return true
		}
	}
	return false
}

func (h RestackWorktreeHold) warning() string {
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

func managedWorktreeHoldReason(ctx *app.Context, path string) string {
	eng := ctx.Engine
	if eng.WorktreeRebaseInProgress(ctx.Context, path) {
		return restackWorktreeHoldReasonRebase
	}
	hasChanges, err := eng.WorktreeHasUncommittedChanges(ctx.Context, path)
	if err != nil {
		return fmt.Sprintf("could not be inspected: %v", err)
	}
	if hasChanges {
		return restackWorktreeHoldReasonUncommittedChanges
	}
	return ""
}

func branchWorktreeHoldReason(ctx *app.Context, branch, path string) string {
	eng := ctx.Engine
	if eng.WorktreeRebaseInProgress(ctx.Context, path) {
		return restackWorktreeHoldReasonRebase
	}
	hasChanges, err := eng.WorktreeHasTrackedChanges(ctx.Context, path)
	if err != nil {
		return fmt.Sprintf("could not be inspected: %v", err)
	}
	if hasChanges {
		return restackWorktreeHoldReasonUncommittedChanges
	}
	if collides, known := eng.UntrackedCollision(ctx.Context, branch); collides || !known {
		if !known {
			return restackWorktreeHoldReasonUntrackedCheckFailed
		}
		return restackWorktreeHoldReasonUntrackedCollision
	}
	return ""
}
