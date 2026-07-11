// Package absorb provides functionality for absorbing staged changes into commits downstack.
package absorb

import (
	"fmt"
	"maps"
	"strings"

	"github.com/getstackit/stackit/internal/actions"
	"github.com/getstackit/stackit/internal/actions/validation"
	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/internal/output"
)

const (
	absorbStashMarker         = "stackit-absorb-temp"
	absorbStashStagedMarker   = absorbStashMarker + "-staged"
	absorbStashUnstagedMarker = absorbStashMarker + "-unstaged"
	unknown                   = "unknown"
)

// Options contains options for the absorb command
type Options struct {
	All     bool
	DryRun  bool
	Force   bool
	Patch   bool
	JSON    bool // Output machine-readable JSON summary
	Restack RestackMode
}

// Action performs the absorb operation
func Action(ctx *app.Context, opts Options, handler Handler) error {
	eng := ctx.Engine
	out := ctx.Output

	// Use null handler if none provided
	if handler == nil {
		handler = &NullHandler{}
	}
	defer handler.Cleanup()

	handler.Start(opts.DryRun)

	// Validate preconditions
	if err := validation.AbsorbChain(ctx.Context, eng, "absorb into").Validate(); err != nil {
		return err
	}
	currentBranch := eng.CurrentBranch()
	opts.Restack = NormalizeRestackMode(opts.Restack)
	if err := opts.Restack.Validate(); err != nil {
		return err
	}

	// Take snapshot before modifying the repository
	snapshotOpts := actions.NewSnapshot("absorb",
		actions.WithFlag(opts.All, "--all"),
		actions.WithFlag(opts.DryRun, "--dry-run"),
		actions.WithFlag(opts.Force, "--force"),
		actions.WithFlag(opts.Patch, "--patch"),
		actions.WithFlagValue("--restack", string(opts.Restack)),
	)
	actions.TakeBestEffortSnapshot(ctx, snapshotOpts)

	// Build a StackGraph for efficient traversals
	graph := eng.Graph(engine.SortStrategyAlphabetical)

	// Check if there are staged changes (before handling flags)
	_, err := eng.HasStagedChanges(ctx.Context)
	if err != nil {
		return fmt.Errorf("failed to check staged changes: %w", err)
	}

	// Handle staging flags. Unlike create/modify, --all stages tracked changes
	// only (git add -u): untracked files can never be absorbed. When combined
	// with --patch, --all wins, matching the pre-existing precedence.
	stagingOpts := git.StagingOptions{
		Update: opts.All,
		Patch:  opts.Patch && !opts.All,
	}
	if err := ctx.Engine.StageChanges(ctx.Context, stagingOpts); err != nil {
		return err
	}

	// Re-check staged changes after flags
	hasStaged, err := eng.HasStagedChanges(ctx.Context)
	if err != nil {
		return fmt.Errorf("failed to check staged changes: %w", err)
	}
	if !hasStaged {
		out.Info("Nothing to absorb.")
		handler.Complete(Result{})
		return nil
	}

	// Parse staged hunks
	hunks, err := eng.ParseStagedHunks(ctx.Context)
	if err != nil {
		return fmt.Errorf("failed to parse staged hunks: %w", err)
	}

	if len(hunks) == 0 {
		out.Info("Nothing to absorb.")
		handler.Complete(Result{})
		return nil
	}
	originalHunks := hunks

	// Get all commits downstack from current branch
	// We need commits from all branches downstack, not just current branch
	downstackBranches := graph.Range(*currentBranch, engine.StackRange{RecursiveParents: true})
	// Include current branch (prepend since Range returns ancestors oldest-to-nearest)
	downstackBranches = engine.BranchesOf(*currentBranch).Concat(downstackBranches)

	// Terminate downstack search if a scope boundary is hit
	currentScope := currentBranch.GetScope()
	if currentScope.IsDefined() {
		limitedDownstack := engine.NewBranchesBuilder(len(downstackBranches))
		for _, branch := range downstackBranches {
			if branch.IsTrunk() || !branch.GetScope().Equal(currentScope) {
				break
			}
			limitedDownstack.Add(branch)
		}
		downstackBranches = limitedDownstack.Build()
	}

	// Get all commit SHAs from downstack branches (newest to oldest)
	commitsByBranch := eng.BatchCommits(downstackBranches, engine.CommitFormatSHA)
	commitSHAs := []string{}
	for _, branch := range downstackBranches {
		// BatchCommits returns newest to oldest per branch, matching our search
		// order. The batch reader swallows errors as nil; absorb must not route
		// hunks against an incomplete commit list, so re-read any empty non-trunk
		// branch individually to distinguish "legitimately empty" from "error".
		commits := commitsByBranch[branch.GetName()]
		if len(commits) == 0 && !branch.IsTrunk() {
			var err error
			commits, err = branch.GetAllCommits(engine.CommitFormatSHA)
			if err != nil {
				return fmt.Errorf("failed to get commits for branch %s: %w", branch.GetName(), err)
			}
		}
		commitSHAs = append(commitSHAs, commits...)
	}

	// Find target commit for each hunk
	candidateTargets := []git.HunkTarget{}
	absorbedTargets := []git.HunkTarget{}
	unabsorbedHunks := []Unabsorbable{}

	for _, hunk := range hunks {
		switch {
		case hunk.Binary:
			unabsorbedHunks = append(unabsorbedHunks, Unabsorbable{Hunk: hunk, Reason: ReasonBinary})
			continue
		case hunk.IsNewFile:
			unabsorbedHunks = append(unabsorbedHunks, Unabsorbable{Hunk: hunk, Reason: ReasonNewFile})
			continue
		case hunk.IsDeletedFile:
			unabsorbedHunks = append(unabsorbedHunks, Unabsorbable{Hunk: hunk, Reason: ReasonDeletedFile})
			continue
		}

		commitSHA, commitIndex, err := eng.FindTargetCommitForHunk(hunk, commitSHAs)
		if err != nil {
			return fmt.Errorf("failed to find target commit for hunk: %w", err)
		}

		if commitSHA == "" {
			// Hunk commutes with all commits - can't be absorbed
			unabsorbedHunks = append(unabsorbedHunks, Unabsorbable{Hunk: hunk, Reason: ReasonCommutesWithAll})
			continue
		}

		candidateTargets = append(candidateTargets, git.HunkTarget{
			Hunk:        hunk,
			CommitSHA:   commitSHA,
			CommitIndex: commitIndex,
		})
	}

	// Group hunks by branch, then by commit. Resolve every target commit's
	// owning branch in one batched scan instead of a git-log sweep per hunk.
	commitBranches := eng.FindBranchesForCommits(targetCommitSHAs(candidateTargets))
	hunksByBranch := make(map[string]map[string][]git.Hunk)
	for _, target := range candidateTargets {
		branchName := commitBranches[target.CommitSHA]
		if branchName == "" {
			unabsorbedHunks = append(unabsorbedHunks, Unabsorbable{Hunk: target.Hunk, Reason: ReasonUnknownBranch})
			continue
		}
		if hunksByBranch[branchName] == nil {
			hunksByBranch[branchName] = make(map[string][]git.Hunk)
		}
		hunksByBranch[branchName][target.CommitSHA] = append(hunksByBranch[branchName][target.CommitSHA], target.Hunk)
		absorbedTargets = append(absorbedTargets, target)
	}

	// Check if any target branches are locked or frozen
	for branchName := range hunksByBranch {
		branch := eng.GetBranch(branchName)
		if err := branch.EnsureCanModify(); err != nil {
			return err
		}
	}

	if len(hunksByBranch) == 0 {
		if len(unabsorbedHunks) > 0 {
			out.Warn("The following hunks could not be absorbed:")
			for _, unabsorbable := range unabsorbedHunks {
				hunk := unabsorbable.Hunk
				start, end := hunkLineRange(hunk)
				out.Info("  %s (lines %d-%d) [%s]", hunk.File, start, end, unabsorbable.Reason.Description())
			}
		} else {
			out.Info("Nothing to absorb.")
		}
		handler.Complete(Result{Unabsorbed: len(unabsorbedHunks)})
		return nil
	}

	// Print dry-run output or confirmation
	if opts.DryRun {
		// Flatten for printing
		flatHunksByCommit := make(map[string][]git.Hunk)
		for _, branchHunks := range hunksByBranch {
			maps.Copy(flatHunksByCommit, branchHunks)
		}
		printDryRunOutput(flatHunksByCommit, unabsorbedHunks, eng, out)

		// Output JSON if requested (works with dry-run for preview)
		if opts.JSON {
			newFiles, err := ctx.Engine.GetUntrackedFiles(ctx.Context)
			if err != nil {
				out.Debug("Failed to get untracked files: %v", err)
			}

			planJSON, err := GeneratePlanJSON(
				currentBranch.GetName(),
				absorbedTargets,
				unabsorbedHunks,
				newFiles,
				eng,
			)
			if err != nil {
				return fmt.Errorf("failed to generate JSON: %w", err)
			}
			out.Print(string(planJSON))
			out.Newline()
		}

		// Don't call Complete() for dry-run - no actual changes made
		return nil
	}

	// Print what will be absorbed
	flatHunksByCommit := make(map[string][]git.Hunk)
	for _, branchHunks := range hunksByBranch {
		maps.Copy(flatHunksByCommit, branchHunks)
	}
	printAbsorbPlan(flatHunksByCommit, unabsorbedHunks, eng, out)

	// Prompt for confirmation if not --force
	if !opts.Force && handler.IsInteractive() {
		confirmed, err := handler.PromptConfirm("Apply these changes to the commits?")
		if err != nil {
			return fmt.Errorf("confirmation canceled: %w", err)
		}
		if !confirmed {
			out.Info("Absorb canceled")
			handler.Complete(Result{})
			return nil
		}
	} else if !opts.Force && !handler.IsInteractive() {
		// Non-interactive without force: default to no
		out.Info("Non-interactive mode: skipping absorb (use --force to override)")
		handler.Complete(Result{})
		return nil
	}

	// Stash staged and unstaged changes separately to avoid reintroducing absorbed hunks.
	hasUnstaged, err := eng.HasUnstagedChanges(ctx.Context)
	if err != nil {
		return fmt.Errorf("failed to check unstaged changes: %w", err)
	}
	hasUntracked, err := eng.HasUntrackedFiles(ctx.Context)
	if err != nil {
		return fmt.Errorf("failed to check untracked files: %w", err)
	}
	hasUnstagedOrUntracked := hasUnstaged || hasUntracked

	var (
		stashedStaged   bool
		stashedUnstaged bool
		stagedFallback  bool
		absorbSucceeded bool
		unstagedPatch   string
	)

	if hasStaged {
		stashOutput, stashErr := eng.StashPushStaged(ctx.Context, absorbStashStagedMarker)
		if stashErr != nil {
			// Some Git versions can return a non-zero exit while still creating the stash entry.
			stashList, listErr := eng.StashList(ctx.Context)
			if listErr == nil {
				if findStashRef(stashList, absorbStashStagedMarker) != "" {
					stashedStaged = true
				}
			}
			// Fallback: capture the unstaged delta as a patch, then reset staged changes.
			stagedFallback = true
		} else if !strings.Contains(stashOutput, "No local changes to save") {
			stashedStaged = true
		}
	}

	if stagedFallback {
		// Capture the unstaged (index->worktree) delta before `reset --hard`
		// wipes it. We reapply this patch with `git apply` (working tree only)
		// after the rewrite instead of popping a `--keep-index` stash: that pop
		// three-way merges against a tree already containing the absorbed
		// content and writes literal conflict markers into the user's files.
		// `git apply` applies atomically or not at all, so it never conflicts.
		// `reset --hard` leaves untracked files in place, so they need no
		// handling here.
		//
		// Capture with `git diff --binary`: a tracked binary file with unstaged
		// edits would otherwise diff to only a "Binary files ... differ"
		// placeholder that `git apply` cannot reapply, silently losing the edit
		// (and, because git apply is atomic per invocation, poisoning any
		// coexisting text edits too).
		if hasUnstaged {
			diff, diffErr := eng.GetUnstagedDiffBinary(ctx.Context)
			if diffErr != nil {
				return fmt.Errorf("failed to capture unstaged changes: %w", diffErr)
			}
			unstagedPatch = diff
		}

		if err := eng.ResetHard(ctx.Context, "HEAD"); err != nil {
			return fmt.Errorf("failed to reset staged changes: %w", err)
		}
	} else if hasUnstagedOrUntracked {
		stashOutput, stashErr := eng.StashPush(ctx.Context, absorbStashUnstagedMarker)
		if stashErr != nil {
			return fmt.Errorf("failed to stash unstaged changes: %w", stashErr)
		}
		if !strings.Contains(stashOutput, "No local changes to save") {
			stashedUnstaged = true
		}
	}

	defer func() {
		restoreStashedState(ctx.Context, eng, out, restoreParams{
			stashedStaged:   stashedStaged,
			stashedUnstaged: stashedUnstaged,
			stagedFallback:  stagedFallback,
			absorbSucceeded: absorbSucceeded,
			unstagedPatch:   unstagedPatch,
			unabsorbedHunks: unabsorbedHunks,
			originalHunks:   originalHunks,
		})
	}()

	// Track the oldest modified branch to know where to start restacking from
	var oldestModifiedBranch string

	// Get branches in topological order (bottom-up)
	allBranches := eng.AllBranches()
	sortedBranches := eng.SortBranchesTopologically(allBranches)

	for _, branch := range sortedBranches {
		branchHunks, ok := hunksByBranch[branch.GetName()]
		if !ok {
			continue
		}

		if oldestModifiedBranch == "" {
			oldestModifiedBranch = branch.GetName()
		}

		// Apply all hunks for this branch together
		if err := eng.ApplyHunksToBranch(ctx.Context, branch, branchHunks); err != nil {
			return fmt.Errorf("failed to apply hunks to branch %s: %w", branch.GetName(), err)
		}

		for commitSHA := range branchHunks {
			handler.OnApply(branch.GetName(), commitSHA[:8])
			out.Info("Absorbed changes into commit %s in %s", commitSHA[:8], output.BranchName(branch.GetName()))
		}
	}

	// Warn about unabsorbed hunks
	if len(unabsorbedHunks) > 0 {
		out.Warn("The following hunks could not be absorbed:")
		for _, unabsorbable := range unabsorbedHunks {
			hunk := unabsorbable.Hunk
			start, end := hunkLineRange(hunk)
			out.Info("  %s (lines %d-%d) [%s]", hunk.File, start, end, unabsorbable.Reason.Description())
		}
	}

	// Absorb rewrites history via raw git operations (cherry-pick + reset on
	// every modified branch), which engine doesn't observe through its own
	// mutation paths. Rebuild reconciles cached ParentBranchRevision and
	// branch tips with the post-rewrite state on disk.
	if err := eng.Rebuild(""); err != nil {
		return fmt.Errorf("failed to refresh engine after absorb: %w", err)
	}

	// Restack branches according to mode
	if oldestModifiedBranch != "" {
		// Rebuild graph with fresh engine state
		graph = eng.Graph(engine.SortStrategyAlphabetical)
		upstackBranches := selectRestackBranches(graph, eng, opts.Restack, currentBranch.GetName(), oldestModifiedBranch, currentScope)

		if len(upstackBranches) > 0 {
			if err := actions.RestackBranches(ctx, upstackBranches); err != nil {
				return fmt.Errorf("failed to restack upstack branches: %w", err)
			}
		}

		// Narrower modes can leave descendants of the rewritten commits on
		// stale parents with no visible signal; tell the user how to finish.
		if opts.Restack != RestackAll {
			allUpstack := selectRestackBranches(graph, eng, RestackAll, currentBranch.GetName(), oldestModifiedBranch, currentScope)
			if skipped := len(allUpstack) - len(upstackBranches); skipped > 0 {
				out.Warn(
					"Skipped restacking %d %s above the rewritten commits; run 'stackit restack --upstack' to update them.",
					skipped,
					actions.Pluralize("branch", skipped),
				)
			}
		}
	}

	absorbSucceeded = true
	handler.Complete(Result{
		Absorbed:    len(absorbedTargets),
		Unabsorbed:  len(unabsorbedHunks),
		BranchCount: len(hunksByBranch),
	})

	// Output JSON summary if requested
	if opts.JSON {
		newFiles, err := ctx.Engine.GetUntrackedFiles(ctx.Context)
		if err != nil {
			out.Debug("Failed to get untracked files: %v", err)
		}

		jsonOutput, err := GeneratePlanJSON(
			currentBranch.GetName(),
			absorbedTargets,
			unabsorbedHunks,
			newFiles,
			eng,
		)
		if err != nil {
			return fmt.Errorf("failed to generate JSON: %w", err)
		}
		out.Print(string(jsonOutput))
		out.Newline()
	}

	return nil
}
