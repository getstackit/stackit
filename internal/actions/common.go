// Package actions provides high-level business logic for CLI commands.
//
// Each action corresponds to a stackit command (create, submit, sync, etc.)
// and orchestrates operations across the engine, git, and github packages.
//
// Key patterns:
//   - Actions accept runtime.Context which provides Engine, Splog, and other dependencies
//   - Actions are stateless - all state is managed through the Engine interface
//   - Actions handle user interaction through the tui package
//
// Dependencies:
//   - engine: Core branch state management
//   - git: Low-level git operations
//   - tui: User interface and prompts
package actions

import (
	"context"
	"fmt"

	"stackit.dev/stackit/internal/app"
	"stackit.dev/stackit/internal/config"
	"stackit.dev/stackit/internal/engine"
	"stackit.dev/stackit/internal/git"
	"stackit.dev/stackit/internal/tui/style"
)

// Restacker is a minimal interface needed for restacking branches
type Restacker interface {
	engine.BranchReader
	engine.SyncManager
}

// RestackProgressCallback is called for each branch during restack
// branchName: the branch being processed
// result: the restack result (Done, Unneeded, Conflict)
// newRev: the new revision if restacked (empty if not applicable)
// conflict: true if this is a conflict
// lockReason: why the branch is locked (empty if not locked)
// frozen: true if the branch is frozen
type RestackProgressCallback func(branchName string, result engine.RestackResult, newRev string, conflict bool, lockReason engine.LockReason, frozen bool)

// RestackBranches restacks a list of branches using the engine's batch restack method
func RestackBranches(ctx *app.Context, branches []engine.Branch) error {
	return RestackBranchesWithHandler(ctx, branches, nil)
}

// RestackBranchesWithHandler restacks branches with optional progress callback
func RestackBranchesWithHandler(ctx *app.Context, branches []engine.Branch, callback RestackProgressCallback) error {
	batchResult, err := ctx.Engine.RestackBranches(ctx.Context, branches)
	if err != nil {
		if batchResult.ConflictBranch != "" {
			// Report the conflict via callback if provided
			if callback != nil {
				callback(batchResult.ConflictBranch, engine.RestackConflict, "", true, engine.LockReasonNone, false)
			}

			continuation := &config.ContinuationState{
				BranchesToRestack:     batchResult.RemainingBranches,
				RebasedBranchBase:     batchResult.RebasedBranchBase,
				CurrentBranchOverride: batchResult.ConflictBranch,
			}

			if err := config.PersistContinuationState(ctx.RepoRoot, continuation); err != nil {
				return fmt.Errorf("failed to persist continuation: %w", err)
			}

			if err := PrintConflictStatus(ctx, batchResult.ConflictBranch); err != nil {
				return fmt.Errorf("failed to print conflict status: %w", err)
			}
		}
		return fmt.Errorf("batch restack failed: %w", err)
	}

	// Handle conflicts even when no error was returned
	if batchResult.ConflictBranch != "" {
		// Report the conflict via callback if provided
		if callback != nil {
			callback(batchResult.ConflictBranch, engine.RestackConflict, "", true, engine.LockReasonNone, false)
		}

		continuation := &config.ContinuationState{
			BranchesToRestack:     batchResult.RemainingBranches,
			RebasedBranchBase:     batchResult.RebasedBranchBase,
			CurrentBranchOverride: batchResult.ConflictBranch,
		}

		if err := config.PersistContinuationState(ctx.RepoRoot, continuation); err != nil {
			return fmt.Errorf("failed to persist continuation: %w", err)
		}

		if err := PrintConflictStatus(ctx, batchResult.ConflictBranch); err != nil {
			return fmt.Errorf("failed to print conflict status: %w", err)
		}

		return fmt.Errorf("restack stopped due to conflict on %s", batchResult.ConflictBranch)
	}

	currentBranch := ctx.Engine.CurrentBranch()
	currentBranchName := ""
	if currentBranch != nil {
		currentBranchName = currentBranch.GetName()
	}

	for _, branch := range branches {
		branchName := branch.GetName()
		result, exists := batchResult.Results[branchName]
		if !exists {
			continue // Skip branches not processed (e.g., trunk)
		}

		// Get new revision if available
		newRev := ""
		if result.Result == engine.RestackDone {
			if rev, err := branch.GetRevision(); err == nil {
				if len(rev) > 7 {
					newRev = rev[:7]
				} else {
					newRev = rev
				}
			}
		}

		// Report via callback if provided
		if callback != nil {
			callback(branchName, result.Result, newRev, false, result.LockReason, result.Frozen)
			continue // Skip splog output when using callback handler
		}

		// Log via splog only when no callback is provided (backward compatibility)
		if result.Reparented {
			isCurrent := branchName == currentBranchName
			ctx.Splog.Info("Reparented %s from %s to %s (parent was merged/deleted).",
				style.ColorBranchName(branchName, isCurrent),
				style.ColorBranchName(result.OldParent, false),
				style.ColorBranchName(result.NewParent, false))
		}

		switch result.Result {
		case engine.RestackDone:
			parent := branch.GetParent()
			parentName := ""
			if parent == nil {
				parentName = ctx.Engine.Trunk().GetName()
			} else {
				parentName = parent.GetName()
			}
			isCurrent := branchName == currentBranchName
			ctx.Splog.Info("Restacked %s on %s.",
				style.ColorBranchName(branchName, isCurrent),
				style.ColorBranchName(parentName, false))
		case engine.RestackConflict:
			// This should not happen since conflicts are handled at the batch level
			return fmt.Errorf("unexpected conflict in batch result for branch %s", branchName)
		case engine.RestackUnneeded:
			switch {
			case !branch.CanModify():
				if branch.IsLocked() {
					ctx.Splog.Info("Did not restack branch %s because it is locked: %s", style.ColorBranchName(branchName, branchName == currentBranchName), branch.GetLockReason())
				} else {
					ctx.Splog.Info("Did not restack branch %s because it is frozen.", style.ColorBranchName(branchName, branchName == currentBranchName))
				}
			case branch.IsTrunk():
				ctx.Splog.Info("%s does not need to be restacked.", style.ColorBranchName(branchName, false))
			default:
				parent := branch.GetParent()
				parentName := ""
				if parent == nil {
					parentName = ctx.Engine.Trunk().GetName()
				} else {
					parentName = parent.GetName()
				}
				isCurrent := branchName == currentBranchName
				ctx.Splog.Info("%s does not need to be restacked on %s.",
					style.ColorBranchName(branchName, isCurrent),
					style.ColorBranchName(parentName, false))
			}
		}
	}

	return nil
}

// PluralSuffix returns "es" if plural is true, otherwise empty string
func PluralSuffix(plural bool) string {
	if plural {
		return "es"
	}
	return ""
}

// Pluralize returns the word with "ren" suffix if count != 1 (specific to "child" -> "children")
func Pluralize(word string, count int) string {
	if count == 1 {
		return word
	}
	return word + "ren" // "child" -> "children"
}

// ShouldDeleteBranch checks if a branch should be deleted
func ShouldDeleteBranch(ctx context.Context, branchName string, eng engine.BranchStatus, force bool) (bool, string) {
	status, err := eng.GetDeletionStatus(ctx, branchName)
	if err != nil {
		return false, ""
	}

	if status.SafeToDelete {
		return true, status.Reason
	}

	if force {
		return false, ""
	}

	// Interactive prompting not yet implemented
	return false, ""
}

type deleteBranchCachedEngine interface {
	engine.StackNavigator
	engine.BranchInfo
	engine.GitDiffer
}

// ShouldDeleteBranchCached checks if a branch should be deleted using pre-fetched metadata and revisions
func ShouldDeleteBranchCached(ctx context.Context, branchName string, eng deleteBranchCachedEngine, force bool, meta *git.Meta, revisions map[string]string, mergedBranches map[string]bool) (bool, string) {
	// 1. Check PR info from cached metadata
	if meta != nil && meta.PrInfo != nil {
		const (
			prStateClosed = "CLOSED"
			prStateMerged = "MERGED"
		)
		if meta.PrInfo.State != nil {
			if *meta.PrInfo.State == prStateClosed {
				return true, fmt.Sprintf("%s is closed on GitHub", branchName)
			}
			if *meta.PrInfo.State == prStateMerged {
				base := ""
				if meta.PrInfo.Base != nil {
					base = *meta.PrInfo.Base
				}
				if base == "" {
					base = eng.Trunk().GetName()
				}
				return true, fmt.Sprintf("%s is merged into %s", branchName, base)
			}
		}
	}

	// 2. Check if merged into trunk
	trunkName := eng.Trunk().GetName()
	if mergedBranches != nil && mergedBranches[branchName] {
		return true, fmt.Sprintf("%s is merged into %s", branchName, trunkName)
	}

	// 3. Check if empty
	// Need parent revision
	var parentRev string
	branch := eng.GetBranch(branchName)
	parent := branch.GetParent()
	parentName := trunkName
	if parent != nil {
		parentName = parent.GetName()
	}

	// Use cached revisions to avoid eng.Git().GetRevision calls
	if rev, ok := revisions[parentName]; ok {
		parentRev = rev
	} else {
		// Fallback
		rev, err := eng.GetRevisionForName(parentName)
		if err == nil {
			parentRev = rev
		}
	}

	if parentRev != "" {
		empty, err := eng.IsDiffEmpty(ctx, branchName, parentRev)
		if err == nil && empty { // IsDiffEmpty returns true if no diff
			// Only delete empty branches if they have a PR
			if meta != nil && meta.PrInfo != nil && meta.PrInfo.Number != nil && *meta.PrInfo.Number != 0 {
				return true, fmt.Sprintf("%s is empty", branchName)
			}
		}
	}

	if force {
		return false, ""
	}

	return false, ""
}

// PluralIt returns "them" if plural is true, otherwise "it"
func PluralIt(plural bool) string {
	if plural {
		return "them"
	}
	return "it"
}

// SnapshotOption is a function that modifies SnapshotOptions
type SnapshotOption func(*engine.SnapshotOptions)

// NewSnapshot creates a new SnapshotOptions with the given command and options
func NewSnapshot(command string, options ...SnapshotOption) engine.SnapshotOptions {
	opts := engine.SnapshotOptions{
		Command: command,
		Args:    []string{},
	}
	for _, option := range options {
		option(&opts)
	}
	return opts
}

// WithArg appends a single argument if it's not empty
func WithArg(arg string) SnapshotOption {
	return func(opts *engine.SnapshotOptions) {
		if arg != "" {
			opts.Args = append(opts.Args, arg)
		}
	}
}

// WithArgs appends multiple arguments
func WithArgs(args ...string) SnapshotOption {
	return func(opts *engine.SnapshotOptions) {
		opts.Args = append(opts.Args, args...)
	}
}

// WithFlag appends a flag if condition is true
func WithFlag(condition bool, flag string) SnapshotOption {
	return func(opts *engine.SnapshotOptions) {
		if condition {
			opts.Args = append(opts.Args, flag)
		}
	}
}

// WithFlagValue appends a flag with a value if the value is not empty
func WithFlagValue(flag string, value string) SnapshotOption {
	return func(opts *engine.SnapshotOptions) {
		if value != "" {
			opts.Args = append(opts.Args, flag, value)
		}
	}
}

// PrintConflictStatus displays conflict information and instructions to the user
func PrintConflictStatus(ctx *app.Context, branchName string) error {
	reader := ctx.Reader()
	splog := ctx.Splog

	msg := style.ColorRed(fmt.Sprintf("Hit conflict restacking %s", branchName))
	splog.Info("%s", msg)
	splog.Newline()

	// Get unmerged files
	unmergedFiles, err := reader.GetUnmergedFiles(ctx.Context)
	if err == nil && len(unmergedFiles) > 0 {
		splog.Info("%s", style.ColorYellow("Unmerged files:"))
		for _, file := range unmergedFiles {
			splog.Info("%s", style.ColorRed(file))
		}
		splog.Newline()
	}

	// Get rebase head
	rebaseHead, err := reader.GetRebaseHead()
	if err == nil && rebaseHead != "" {
		rebaseHeadShort := rebaseHead
		if len(rebaseHead) > 7 {
			rebaseHeadShort = rebaseHead[:7]
		}
		msg := style.ColorYellow(fmt.Sprintf("You are here (resolving %s):", rebaseHeadShort))
		splog.Info("%s", msg)
		// Could show log here if needed
		splog.Newline()
	}

	splog.Info("%s", style.ColorYellow("To fix and continue your previous Stackit command:"))
	splog.Info("(1) resolve the listed merge conflicts")
	splog.Info("(2) mark them as resolved with %s", style.ColorCyan("stackit add ."))
	splog.Info("(3) run %s to continue executing your previous Stackit command", style.ColorCyan("stackit continue"))
	splog.Info("It's safe to cancel the ongoing rebase with %s.", style.ColorCyan("git rebase --abort"))

	return nil
}
