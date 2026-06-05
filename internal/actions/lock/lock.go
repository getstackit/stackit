// Package lock provides functionality for locking and unlocking branches in a stack.
package lock

import (
	"fmt"

	"github.com/getstackit/stackit/internal/actions"
	"github.com/getstackit/stackit/internal/actions/submit"
	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/output"
)

// Action locks the specified branch and all branches downstack of it
func Action(ctx *app.Context, branchName string, handler Handler) error {
	if handler == nil {
		handler = &NullHandler{}
	}
	defer handler.Cleanup()

	eng := ctx.Engine
	out := ctx.Output

	branch := eng.GetBranch(branchName)
	if branch.IsTrunk() {
		return fmt.Errorf("cannot lock trunk branch %s", branchName)
	}

	if !branch.IsTracked() {
		return fmt.Errorf("branch %s is not tracked by stackit", branchName)
	}

	// Build StackGraph for efficient traversals
	graph := eng.Graph(engine.SortStrategyAlphabetical)

	// Get downstack (ancestors including current)
	branches := graph.Range(branch, engine.StackRange{
		RecursiveParents: true,
		IncludeCurrent:   true,
	})

	// Check for unpushed commits
	unpushedBranches := []string{}
	remoteStatuses := eng.ReadBranchRemoteStatuses(ctx.Context, branches)
	for _, b := range branches {
		if b.IsTrunk() {
			continue
		}
		status := remoteStatuses[b.GetName()]
		if !status.Matches() {
			if status.Ahead() || status.MissingRemote() || status.Diverged() {
				unpushedBranches = append(unpushedBranches, b.GetName())
			}
		}
	}

	if len(unpushedBranches) > 0 && handler.IsInteractive() {
		out.Warn("The following branches have unpushed commits:")
		for _, b := range unpushedBranches {
			out.Warn("  - %s", b)
		}
		confirm, err := handler.PromptSubmitBeforeLock(unpushedBranches)
		if err == nil && confirm {
			submitOpts := submit.Options{
				Branch:     branchName,
				StackRange: engine.StackRangeDownstack(true),
				Confirm:    false,
			}
			submitHandler := handler.GetSubmitHandler()
			if err := submit.Action(ctx, submitOpts, submitHandler); err != nil {
				return fmt.Errorf("failed to submit before locking: %w", err)
			}
		}
	}

	affectedBranches := []string{}
	branchesToLock := engine.Branches{}
	for _, b := range branches {
		if b.IsTrunk() {
			continue
		}
		if b.IsLocked() {
			out.Info("Branch %s is already locked.", output.Branch(b.GetName(), b.GetName() == branchName))
			continue
		}
		branchesToLock = branchesToLock.Append(b)
	}

	if len(branchesToLock) > 0 {
		res, err := eng.SetLocked(ctx, branchesToLock, engine.LockReasonUser)
		if err != nil {
			// Report specific errors if some failed
			for name, branchErr := range res.Errors {
				out.Warn("Failed to lock %s: %v", name, branchErr)
			}
			return fmt.Errorf("failed to lock branches: %w", err)
		}

		for _, name := range res.AffectedBranches {
			out.Info("Locked %s.", output.Branch(name, name == branchName))
			affectedBranches = append(affectedBranches, name)
		}
	}

	// Mark branches for PR body update; sync will handle the GitHub API calls
	if err := eng.MarkBranchesForPRBodyUpdate(ctx.Context, affectedBranches); err != nil {
		out.Debug("Failed to mark branches for PR body update: %v", err)
	}
	if err := actions.PushMetadataOnly(ctx, eng, affectedBranches); err != nil {
		out.Debug("Failed to push metadata changes: %v", err)
	}

	return nil
}

// Unlock unlocks the specified branch and all branches upstack of it
func Unlock(ctx *app.Context, branchName string, handler Handler) error {
	if handler == nil {
		handler = &NullHandler{}
	}
	defer handler.Cleanup()

	eng := ctx.Engine
	out := ctx.Output

	branch := eng.GetBranch(branchName)
	if !branch.IsTracked() {
		return fmt.Errorf("branch %s is not tracked by stackit", branchName)
	}

	// Build StackGraph for efficient traversals
	graph := eng.Graph(engine.SortStrategyAlphabetical)

	// Get upstack (descendants including current)
	branches := graph.Range(branch, engine.StackRange{
		IncludeCurrent:    true,
		RecursiveChildren: true,
	})

	// Check if downstack has locked branches and prompt to unlock them if interactive
	downstack := graph.Range(branch, engine.StackRange{
		RecursiveParents: true,
	})

	lockedDownstack := engine.Branches{}
	for _, b := range downstack {
		if !b.IsTrunk() && b.IsLocked() {
			lockedDownstack = lockedDownstack.Append(b)
		}
	}

	if len(lockedDownstack) > 0 && handler.IsInteractive() {
		// Collect branch names for the prompt
		lockedNames := lockedDownstack.Names()

		confirm, err := handler.PromptUnlockDownstack(lockedNames)
		if err == nil && confirm {
			branches = branches.Concat(lockedDownstack)
		}
	}

	affectedBranches := []string{}
	branchesToUnlock := engine.Branches{}
	for _, b := range branches {
		if b.IsTrunk() {
			continue
		}
		if !b.IsLocked() {
			out.Info("Branch %s is already unlocked.", output.Branch(b.GetName(), b.GetName() == branchName))
			continue
		}
		branchesToUnlock = branchesToUnlock.Append(b)
	}

	if len(branchesToUnlock) > 0 {
		res, err := eng.SetLocked(ctx, branchesToUnlock, engine.LockReasonNone)
		if err != nil {
			// Report specific errors if some failed
			for name, branchErr := range res.Errors {
				out.Warn("Failed to unlock %s: %v", name, branchErr)
			}
			return fmt.Errorf("failed to unlock branches: %w", err)
		}

		for _, name := range res.AffectedBranches {
			out.Info("Unlocked %s.", output.Branch(name, name == branchName))
			affectedBranches = append(affectedBranches, name)
		}
	}

	// Mark branches for PR body update; sync will handle the GitHub API calls
	if err := eng.MarkBranchesForPRBodyUpdate(ctx.Context, affectedBranches); err != nil {
		out.Debug("Failed to mark branches for PR body update: %v", err)
	}
	if err := actions.PushMetadataOnly(ctx, eng, affectedBranches); err != nil {
		out.Debug("Failed to push metadata changes: %v", err)
	}

	return nil
}
