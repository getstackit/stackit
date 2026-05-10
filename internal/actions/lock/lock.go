// Package lock provides functionality for locking and unlocking branches in a stack.
package lock

import (
	"fmt"

	"github.com/getstackit/stackit/internal/actions"
	"github.com/getstackit/stackit/internal/actions/submit"
	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/tui/style"
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
	if err := eng.PopulateRemoteShas(); err == nil {
		for _, b := range branches {
			if b.IsTrunk() {
				continue
			}
			status, err := eng.GetBranchRemoteStatus(b)
			if err == nil && !status.Matches() {
				if status.Ahead() || status.MissingRemote() || status.Diverged() {
					unpushedBranches = append(unpushedBranches, b.GetName())
				}
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
	branchesToLock := []engine.Branch{}
	for _, b := range branches {
		if b.IsTrunk() {
			continue
		}
		if b.IsLocked() {
			out.Info("Branch %s is already locked.", style.ColorBranchName(b.GetName(), b.GetName() == branchName))
			continue
		}
		branchesToLock = append(branchesToLock, b)
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
			out.Info("Locked %s.", style.ColorBranchName(name, name == branchName))
			affectedBranches = append(affectedBranches, name)
		}
	}

	// Push metadata changes to remote and update PRs to trigger CI re-evaluation
	if err := actions.PushMetadataAndSyncPRs(ctx, affectedBranches); err != nil {
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

	lockedDownstack := []engine.Branch{}
	for _, b := range downstack {
		if !b.IsTrunk() && b.IsLocked() {
			lockedDownstack = append(lockedDownstack, b)
		}
	}

	if len(lockedDownstack) > 0 && handler.IsInteractive() {
		// Collect branch names for the prompt
		lockedNames := make([]string, len(lockedDownstack))
		for i, b := range lockedDownstack {
			lockedNames[i] = b.GetName()
		}

		confirm, err := handler.PromptUnlockDownstack(lockedNames)
		if err == nil && confirm {
			branches = append(branches, lockedDownstack...)
		}
	}

	affectedBranches := []string{}
	branchesToUnlock := []engine.Branch{}
	for _, b := range branches {
		if b.IsTrunk() {
			continue
		}
		if !b.IsLocked() {
			out.Info("Branch %s is already unlocked.", style.ColorBranchName(b.GetName(), b.GetName() == branchName))
			continue
		}
		branchesToUnlock = append(branchesToUnlock, b)
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
			out.Info("Unlocked %s.", style.ColorBranchName(name, name == branchName))
			affectedBranches = append(affectedBranches, name)
		}
	}

	// Push metadata changes to remote and update PRs to trigger CI re-evaluation
	if err := actions.PushMetadataAndSyncPRs(ctx, affectedBranches); err != nil {
		out.Debug("Failed to push metadata changes: %v", err)
	}

	return nil
}
