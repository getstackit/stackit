package absorb

import (
	"context"
	"fmt"
	"strings"

	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/git"
)

// IsAbsorbInProgress checks if there's a failed absorb operation that needs cleanup.
// This is detected by checking for:
// 1. Presence of absorb stash marker, OR
// 2. Detached HEAD state with no rebase/merge in progress
func IsAbsorbInProgress(ctx *app.Context) bool {
	// A rebase or merge in progress is owned by the standard conflict-workflow
	// abort (restack/sync/merge). EnterConflictWorkflow deliberately detaches
	// HEAD before the conflict rebase — which writes a "checkout: moving from"
	// reflog entry — and persists continuation + snapshot state for the standard
	// abort to roll back. Absorb's own failure mode is a cherry-pick conflict,
	// which never leaves a rebase-merge/rebase-apply directory. So an in-progress
	// rebase/merge means this is NOT an absorb to clean up. Without this guard the
	// conflict workflow's detach is misread as a mid-absorb failure, routing
	// `stackit abort` to absorb cleanup — which never runs `git rebase --abort`
	// and leaves the repo stuck mid-rebase with stale continuation state.
	if ctx.Engine.IsRebaseInProgress(ctx.Context) || ctx.Engine.IsMergeInProgress(ctx.Context) {
		return false
	}

	// Check for absorb stash marker
	stashList, _ := ctx.Engine.StashList(ctx.Context)
	if strings.Contains(stashList, absorbStashMarker) {
		return true
	}

	// Check for detached HEAD (might be mid-absorb failure)
	// engine.CurrentBranch() returns nil if in detached HEAD.
	if ctx.Engine.CurrentBranch() == nil {
		// In detached HEAD - could be absorb or something else
		// Check reflog for signs of absorb
		reflog, _ := ctx.Engine.GetReflog(ctx.Context, 5, "%gs")
		// If recent reflog shows checkout from a branch (not a commit), likely absorb
		if strings.Contains(reflog, "checkout: moving from") {
			return true
		}
	}

	return false
}

// ShowConflict shows information about what absorb would do and helps diagnose conflicts.
// This is useful when absorb fails and you want to understand what was being attempted.
func ShowConflict(ctx *app.Context) error {
	out := ctx.Output
	eng := ctx.Engine

	// Check if we're in a detached HEAD state (might be mid-absorb failure)
	if eng.CurrentBranch() == nil {
		out.Warn("Repository is in detached HEAD state.")
		out.Info("This may be from a failed absorb. Run 'stackit abort' to recover.")
		out.Info("")

		// Show unmerged files if any
		unmerged, _ := eng.GetUnmergedFiles(ctx.Context)
		if len(unmerged) > 0 {
			out.Info("Unmerged files:")
			for _, file := range unmerged {
				out.Info("  - %s", file)
			}
			out.Info("")
		}
		return nil
	}

	// Check for staged changes
	hasStaged, err := eng.HasStagedChanges(ctx.Context)
	if err != nil {
		return fmt.Errorf("failed to check staged changes: %w", err)
	}

	if !hasStaged {
		out.Info("No staged changes to analyze.")
		out.Info("Stage some changes first, then run 'stackit absorb --dry-run' to see where they would go.")
		return nil
	}

	// Show what absorb would do (dry run)
	out.Info("Analyzing staged changes...\n")

	// Parse staged hunks
	hunks, err := eng.ParseStagedHunks(ctx.Context)
	if err != nil {
		return fmt.Errorf("failed to parse staged hunks: %w", err)
	}

	if len(hunks) == 0 {
		out.Info("No hunks found in staged changes.")
		return nil
	}

	// Show the staged changes
	out.Info("Staged changes:")
	fileHunks := make(map[string][]git.Hunk)
	for _, hunk := range hunks {
		fileHunks[hunk.File] = append(fileHunks[hunk.File], hunk)
	}
	for file, hunks := range fileHunks {
		out.Info("  %s:", file)
		for _, hunk := range hunks {
			out.Info("    lines %d-%d", hunk.NewStart, hunk.NewStart+hunk.NewCount-1)
		}
	}
	out.Info("")

	// Get current branch info
	currentBranchObj := eng.CurrentBranch()
	if currentBranchObj == nil {
		return fmt.Errorf("not on a tracked branch")
	}

	// Build StackGraph for efficient traversals
	graph := eng.Graph(engine.SortStrategyAlphabetical)

	// Get downstack commits
	downstackBranches := graph.Range(*currentBranchObj, engine.StackRange{RecursiveParents: true})
	downstackBranches = engine.BranchesOf(*currentBranchObj).Concat(downstackBranches)

	out.Info("Stack (bottom to top):")
	for i := len(downstackBranches) - 1; i >= 0; i-- {
		branch := downstackBranches[i]
		marker := "  "
		if branch.GetName() == currentBranchObj.GetName() {
			marker = "* "
		}
		out.Info("%s%s", marker, branch.GetName())
	}
	out.Info("")

	out.Info("To see where changes would be absorbed:")
	out.Info("  stackit absorb --dry-run")
	out.Info("")
	out.Info("If absorb fails with conflicts, consider:")
	out.Info("  1. Manually checkout the target branch and apply the change")
	out.Info("  2. Split the change into smaller pieces with 'git add -p'")
	out.Info("  3. Create a new commit instead with 'stackit create'")

	return nil
}

// Abort cleans up from a failed absorb state
func Abort(ctx *app.Context) error {
	out := ctx.Output
	eng := ctx.Engine

	// Check if we're in a detached HEAD state
	if eng.CurrentBranch() != nil {
		// Check for stashed changes from absorb
		stashList, _ := eng.StashList(ctx.Context)
		if strings.Contains(stashList, absorbStashMarker) {
			out.Info("Found stashed changes from previous absorb attempt.")
			restored, err := popAllAbsorbStashes(ctx.Context, eng)
			if err != nil {
				out.Warn("Failed to restore stashed changes: %v", err)
			} else {
				out.Info("Restored %d absorb stash entries.", restored)
			}
			return nil
		}

		out.Info("No absorb operation in progress.")
		return nil
	}

	out.Info("Cleaning up failed absorb operation...")

	// Reset any uncommitted changes
	if err := eng.ResetHard(ctx.Context, "HEAD"); err != nil {
		out.Warn("Failed to reset: %v", err)
	}

	// Find the original branch from reflog
	reflog, _ := eng.GetReflog(ctx.Context, 20, "%gs")
	var originalBranch string
	for line := range strings.SplitSeq(reflog, "\n") {
		if strings.Contains(line, "checkout: moving from") {
			parts := strings.Split(line, "moving from ")
			if len(parts) >= 2 {
				branchPart := strings.Split(parts[1], " to ")
				if len(branchPart) >= 1 {
					originalBranch = branchPart[0]
					break
				}
			}
		}
	}

	if originalBranch != "" && originalBranch != "HEAD" {
		branch := eng.GetBranch(originalBranch)
		if err := eng.CheckoutBranch(ctx.Context, branch); err != nil {
			out.Warn("Failed to return to original branch %s: %v", originalBranch, err)
			out.Info("You can manually checkout your branch with: git checkout <branch-name>")
		} else {
			out.Info("Returned to branch %s", originalBranch)
		}
	} else {
		out.Warn("Could not determine original branch from reflog.")
		out.Info("Use 'git checkout <branch-name>' to return to your branch.")
	}

	// Pop stash if there is one
	stashList, _ := eng.StashList(ctx.Context)
	if strings.Contains(stashList, absorbStashMarker) {
		restored, err := popAllAbsorbStashes(ctx.Context, eng)
		if err != nil {
			out.Warn("Failed to restore stashed changes: %v", err)
		} else {
			out.Info("Restored %d absorb stash entries.", restored)
		}
	}

	out.Info("Abort complete.")
	return nil
}

func popAllAbsorbStashes(gitCtx context.Context, eng engine.Engine) (int, error) {
	restored := 0
	for {
		stashList, err := eng.StashList(gitCtx)
		if err != nil {
			return restored, err
		}

		stashRef := findStashRefByMarkers(
			stashList,
			absorbStashStagedMarker,
			absorbStashUnstagedMarker,
			absorbStashMarker,
		)
		if stashRef == "" {
			return restored, nil
		}

		if err := eng.StashPopRef(gitCtx, stashRef); err != nil {
			return restored, err
		}
		restored++
	}
}
