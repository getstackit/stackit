// Package absorb provides functionality for absorbing staged changes into commits downstack.
package absorb

import (
	"fmt"
	"strings"

	"stackit.dev/stackit/internal/actions"
	"stackit.dev/stackit/internal/app"
	"stackit.dev/stackit/internal/engine"
	"stackit.dev/stackit/internal/git"
	"stackit.dev/stackit/internal/tui/style"
)

const (
	absorbStashMarker = "stackit-absorb-temp"
	unknown           = "unknown"
)

// Options contains options for the absorb command
type Options struct {
	All    bool
	DryRun bool
	Force  bool
	Patch  bool
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

	// Get current branch
	currentBranch := eng.CurrentBranch()
	if currentBranch == nil {
		return fmt.Errorf("not on a branch")
	}

	// Check if current branch is trunk
	if currentBranch.IsTrunk() {
		return fmt.Errorf("cannot absorb into trunk branch %s", currentBranch.GetName())
	}

	if err := currentBranch.EnsureCanModify(); err != nil {
		return err
	}

	// Take snapshot before modifying the repository
	snapshotOpts := actions.NewSnapshot("absorb",
		actions.WithFlag(opts.All, "--all"),
		actions.WithFlag(opts.DryRun, "--dry-run"),
		actions.WithFlag(opts.Force, "--force"),
		actions.WithFlag(opts.Patch, "--patch"),
	)
	if err := eng.TakeSnapshot(snapshotOpts); err != nil {
		// Log but don't fail - snapshot is best effort
		out.Debug("Failed to take snapshot: %v", err)
	}

	// Check if rebase is in progress
	if err := ctx.Git().CheckRebaseInProgress(ctx.Context); err != nil {
		return err
	}

	// Build a StackGraph for efficient traversals
	graph := engine.BuildStackGraph(eng, engine.SortStrategyAlphabetical, nil)

	// Check if there are staged changes (before handling flags)
	_, err := eng.HasStagedChanges(ctx.Context)
	if err != nil {
		return fmt.Errorf("failed to check staged changes: %w", err)
	}

	// Handle staging flags
	stagingOpts := git.StagingOptions{
		All:   opts.All,
		Patch: opts.Patch,
	}
	if err := ctx.Git().StageChanges(ctx.Context, stagingOpts); err != nil {
		return err
	}

	// Re-check staged changes after flags
	hasStaged, err := eng.HasStagedChanges(ctx.Context)
	if err != nil {
		return fmt.Errorf("failed to check staged changes: %w", err)
	}
	if !hasStaged {
		out.Info("Nothing to absorb.")
		return nil
	}

	// Parse staged hunks
	hunks, err := eng.ParseStagedHunks(ctx.Context)
	if err != nil {
		return fmt.Errorf("failed to parse staged hunks: %w", err)
	}

	if len(hunks) == 0 {
		out.Info("Nothing to absorb.")
		return nil
	}

	// Get all commits downstack from current branch
	// We need commits from all branches downstack, not just current branch
	downstackBranches := graph.Range(*currentBranch, engine.StackRange{RecursiveParents: true})
	// Include current branch (prepend since Range returns ancestors oldest-to-nearest)
	downstackBranches = append([]engine.Branch{*currentBranch}, downstackBranches...)

	// Terminate downstack search if a scope boundary is hit
	currentScope := currentBranch.GetScope()
	if currentScope.IsDefined() {
		limitedDownstack := []engine.Branch{}
		for _, branch := range downstackBranches {
			if branch.IsTrunk() || !branch.GetScope().Equal(currentScope) {
				break
			}
			limitedDownstack = append(limitedDownstack, branch)
		}
		downstackBranches = limitedDownstack
	}

	// Get all commit SHAs from downstack branches (newest to oldest)
	commitSHAs := []string{}
	for _, branch := range downstackBranches {
		commits, err := branch.GetAllCommits(engine.CommitFormatSHA)
		if err != nil {
			return fmt.Errorf("failed to get commits for branch %s: %w", branch.GetName(), err)
		}
		// Commits are returned oldest to newest, but we want newest to oldest for search
		for i := len(commits) - 1; i >= 0; i-- {
			commitSHAs = append(commitSHAs, commits[i])
		}
	}

	// Find target commit for each hunk
	hunkTargets := []git.HunkTarget{}
	unabsorbedHunks := []git.Hunk{}

	for _, hunk := range hunks {
		commitSHA, commitIndex, err := eng.FindTargetCommitForHunk(hunk, commitSHAs)
		if err != nil {
			return fmt.Errorf("failed to find target commit for hunk: %w", err)
		}

		if commitSHA == "" {
			// Hunk commutes with all commits - can't be absorbed
			unabsorbedHunks = append(unabsorbedHunks, hunk)
			continue
		}

		hunkTargets = append(hunkTargets, git.HunkTarget{
			Hunk:        hunk,
			CommitSHA:   commitSHA,
			CommitIndex: commitIndex,
		})
	}

	// Group hunks by branch, then by commit
	hunksByBranch := make(map[string]map[string][]git.Hunk)
	for _, target := range hunkTargets {
		branchName, err := eng.FindBranchForCommit(target.CommitSHA)
		if err != nil {
			continue
		}
		if hunksByBranch[branchName] == nil {
			hunksByBranch[branchName] = make(map[string][]git.Hunk)
		}
		hunksByBranch[branchName][target.CommitSHA] = append(hunksByBranch[branchName][target.CommitSHA], target.Hunk)
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
			out.Warn("The following hunks could not be absorbed (they commute with all commits):")
			for _, hunk := range unabsorbedHunks {
				out.Info("  %s (lines %d-%d)", hunk.File, hunk.NewStart, hunk.NewStart+hunk.NewCount-1)
			}
		} else {
			out.Info("Nothing to absorb.")
		}
		return nil
	}

	// Print dry-run output or confirmation
	if opts.DryRun {
		// Flatten for printing
		flatHunksByCommit := make(map[string][]git.Hunk)
		for _, branchHunks := range hunksByBranch {
			for commitSHA, hunks := range branchHunks {
				flatHunksByCommit[commitSHA] = hunks
			}
		}
		printDryRunOutput(flatHunksByCommit, unabsorbedHunks, eng, out)
		return nil
	}

	// Print what will be absorbed
	flatHunksByCommit := make(map[string][]git.Hunk)
	for _, branchHunks := range hunksByBranch {
		for commitSHA, hunks := range branchHunks {
			flatHunksByCommit[commitSHA] = hunks
		}
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
			return nil
		}
	} else if !opts.Force && !handler.IsInteractive() {
		// Non-interactive without force: default to no
		out.Info("Non-interactive mode: skipping absorb (use --force to override)")
		return nil
	}

	// Stash all changes (staged and unstaged) before starting to rewrite commits
	// This ensures a clean working directory for checkouts and prevents losing changes
	stashOutput, stashErr := eng.StashPush(ctx.Context, absorbStashMarker)
	if stashErr == nil && !strings.Contains(stashOutput, "No local changes to save") {
		defer func() {
			// Restore stash after we're done
			_ = eng.StashPop(ctx.Context)
		}()
	}

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
			out.Info("Absorbed changes into commit %s in %s", commitSHA[:8], style.ColorBranchName(branch.GetName(), false))
		}
	}

	// Warn about unabsorbed hunks
	if len(unabsorbedHunks) > 0 {
		out.Warn("The following hunks could not be absorbed (they commute with all commits):")
		for _, hunk := range unabsorbedHunks {
			out.Info("  %s (lines %d-%d)", hunk.File, hunk.NewStart, hunk.NewStart+hunk.NewCount-1)
		}
	}

	// Refresh engine state after modifying branch references directly via git
	if err := eng.Rebuild(""); err != nil {
		return fmt.Errorf("failed to refresh engine after absorb: %w", err)
	}

	// Restack all branches above the oldest modified branch
	if oldestModifiedBranch != "" {
		// Rebuild graph with fresh engine state
		graph = engine.BuildStackGraph(eng, engine.SortStrategyAlphabetical, nil)
		upstackBranches := graph.Range(eng.GetBranch(oldestModifiedBranch), engine.StackRange{RecursiveChildren: true})

		if len(upstackBranches) > 0 {
			if err := actions.RestackBranches(ctx, upstackBranches); err != nil {
				return fmt.Errorf("failed to restack upstack branches: %w", err)
			}
		}
	}

	return nil
}
