package split

import (
	"fmt"
	"slices"

	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/config"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/output"
	"github.com/getstackit/stackit/internal/tui"
	"github.com/getstackit/stackit/internal/tui/style"
	"github.com/getstackit/stackit/internal/utils"
)

// splitByCommitEngine is a minimal interface needed for splitting by commit
type splitByCommitEngine interface {
	engine.BranchReader
	engine.PRManager
	engine.StackRewriter
}

// branchGroup represents a group of commits that will form a branch
type branchGroup struct {
	startIdx int    // Start index in commit list (inclusive)
	endIdx   int    // End index in commit list (exclusive)
	name     string // Branch name (may be auto-derived or user-provided)
}

// splitByCommit splits a branch by selecting commits to keep in the current branch,
// then iteratively grouping remaining commits into new branches.
//
// Algorithm:
//  1. Get all commits on the branch.
//  2. User selects which commits to keep in the current branch (draws a split line).
//  3. For remaining commits, iteratively group them into new branches.
//  4. Auto-derive names for single-commit branches, prompt for multi-commit branches.
//  5. Detach HEAD and return the branch names and points.
func splitByCommit(ctx *app.Context, branchToSplit string, eng splitByCommitEngine, splog output.Output, pattern config.BranchPattern) (*Result, error) {
	// Get commits in both readable and subject formats
	branchToSplitObj := eng.GetBranch(branchToSplit)
	readableCommits, err := branchToSplitObj.GetAllCommits(engine.CommitFormatReadable)
	if err != nil {
		return nil, fmt.Errorf("failed to get commits: %w", err)
	}
	subjectCommits, err := branchToSplitObj.GetAllCommits(engine.CommitFormatSubject)
	if err != nil {
		return nil, fmt.Errorf("failed to get commit subjects: %w", err)
	}

	if len(readableCommits) == 0 {
		return nil, fmt.Errorf("no commits to split")
	}

	if len(readableCommits) == 1 {
		return nil, fmt.Errorf("cannot split a branch with only one commit (use --by-hunk instead)")
	}

	// Show initial info
	splog.Info("Splitting the commits of %s into multiple branches.", style.ColorBranchName(branchToSplit, true))
	branch := eng.GetBranch(branchToSplit)
	prInfo, _ := branch.GetPrInfo()
	if prInfo != nil && prInfo.Number() != nil {
		splog.Info("If any of the new branches keeps the name %s, it will be linked to PR #%d.",
			style.ColorBranchName(branchToSplit, true), *prInfo.Number())
	}
	splog.Info("")

	// Step 1: Select commits to keep in current branch
	splitPoint, err := selectSplitPoint(readableCommits, branchToSplit)
	if err != nil {
		return nil, err
	}

	// If user chose to keep all commits, no split needed
	if splitPoint == len(readableCommits) {
		return nil, fmt.Errorf("no commits selected for split (all commits kept in current branch)")
	}

	// Step 2: Group remaining commits into new branches
	remainingCommits := readableCommits[splitPoint:]
	remainingSubjects := subjectCommits[splitPoint:]

	groups, err := groupRemainingCommits(ctx, remainingCommits, remainingSubjects, pattern, eng, branchToSplit, splog)
	if err != nil {
		return nil, err
	}

	// Build result for engine.ApplySplitToCommits
	// Engine expects:
	// - branchPoints: sorted indices (0, 1, 2, ...) where each branch starts
	// - branchNames: names in REVERSE order of branchPoints (oldest branch name first)
	//
	// Example with 2 commits (newest=0, older=1), splitting at index 1:
	// - branchPoints = [0, 1] (current branch at 0, split branch at 1)
	// - branchNames = [split_name, current_name] (older first)
	branchNames := []string{}
	branchPoints := []int{}

	// Add group names in reverse order (oldest branches first)
	// Groups are ordered by startIdx ascending, so reverse gives us oldest first
	for i := len(groups) - 1; i >= 0; i-- {
		branchNames = append(branchNames, groups[i].name)
	}
	// Add current branch name last (newest branch, has index 0)
	branchNames = append(branchNames, branchToSplit)

	// Build branchPoints in sorted order (0 first, then group points in ascending order)
	branchPoints = append(branchPoints, 0) // Current branch at commit 0
	for i := range groups {
		branchPoints = append(branchPoints, splitPoint+groups[i].startIdx)
	}

	// Detach HEAD to the branch revision
	branchRevision, err := branchToSplitObj.GetRevision()
	if err != nil {
		return nil, fmt.Errorf("failed to get branch revision: %w", err)
	}
	if err := eng.Detach(ctx, branchRevision); err != nil {
		return nil, fmt.Errorf("failed to detach: %w", err)
	}

	return &Result{
		BranchNames:  branchNames,
		BranchPoints: branchPoints,
	}, nil
}

// selectSplitPoint prompts the user to select which commits to keep in the current branch.
// Returns the index of the first commit to split off (commits 0..index-1 stay in current branch).
// Returns len(commits) if user wants to keep all commits (no split).
func selectSplitPoint(readableCommits []string, branchToSplit string) (int, error) {
	if !utils.IsInteractive() {
		return 0, fmt.Errorf("split point must be specified in non-interactive mode")
	}

	// Build choices: commits interleaved with split lines
	// Format:
	//   ─────── keep all (no split) ───────
	//   abc123 First commit (newest)
	//   ─────── split here ───────
	//   def456 Second commit
	//   ─────── split here ───────
	//   ...
	choices := []string{}
	splitLineIndices := make(map[int]int) // choice index -> split point (how many commits to keep)

	// "Keep all" option at top
	keepAllLine := "─────── keep all (no split) ───────"
	choices = append(choices, keepAllLine)
	splitLineIndices[0] = len(readableCommits)

	// Add commits and split lines
	for i, commit := range readableCommits {
		choices = append(choices, fmt.Sprintf("  %s", commit))
		if i < len(readableCommits)-1 {
			splitLine := "─────── split here ───────"
			choices = append(choices, splitLine)
			splitLineIndices[len(choices)-1] = i + 1 // Keep commits 0..i, split from i+1
		}
	}

	// Build options for tui.PromptSelect
	var options []tui.SelectOption
	for i, choice := range choices {
		if splitPoint, ok := splitLineIndices[i]; ok {
			// This is a split line - selectable
			options = append(options, tui.SelectOption{
				Label: choice,
				Value: fmt.Sprintf("%d", splitPoint),
			})
		}
		// Skip commit lines - they're not selectable options
	}

	title := fmt.Sprintf("Select where to split %s (commits above the line stay):", style.ColorBranchName(branchToSplit, true))
	selected, err := tui.PromptSelect(title, options, 0)
	if err != nil {
		return 0, err
	}

	// Parse the split point from the selected value
	var splitPoint int
	if _, err := fmt.Sscanf(selected, "%d", &splitPoint); err != nil {
		return 0, fmt.Errorf("invalid selection")
	}

	return splitPoint, nil
}

// groupRemainingCommits iteratively groups commits into branches.
// For single-commit groups, auto-derives the branch name.
// For multi-commit groups, prompts for a name.
func groupRemainingCommits(
	ctx *app.Context,
	readableCommits []string,
	subjectCommits []string,
	pattern config.BranchPattern,
	eng splitByCommitEngine,
	originalBranch string,
	splog output.Output,
) ([]branchGroup, error) {
	if len(readableCommits) == 0 {
		return nil, nil
	}

	groups := []branchGroup{}
	existingNames := []string{originalBranch} // Track names already used
	offset := 0

	for offset < len(readableCommits) {
		remaining := readableCommits[offset:]
		remainingSubjects := subjectCommits[offset:]

		if len(remaining) == 1 {
			// Single commit - auto-derive name
			name, err := deriveBranchName(ctx, remainingSubjects[0], pattern, existingNames, eng)
			if err != nil {
				return nil, err
			}
			existingNames = append(existingNames, name)

			splog.Info("New branch: %s", style.ColorBranchName(name, false))
			splog.Info("  %s", remaining[0])
			splog.Info("")

			groups = append(groups, branchGroup{
				startIdx: offset,
				endIdx:   offset + 1,
				name:     name,
			})
			offset++
		} else {
			// Multiple commits - ask user to group or split further
			groupSize, err := selectGroupSize(remaining)
			if err != nil {
				return nil, err
			}

			groupReadable := remaining[:groupSize]
			groupSubjects := remainingSubjects[:groupSize]

			var name string
			if groupSize == 1 {
				// Single commit after selection - auto-derive
				name, err = deriveBranchName(ctx, groupSubjects[0], pattern, existingNames, eng)
				if err != nil {
					return nil, err
				}
			} else {
				// Multiple commits - prompt for name
				splog.Info("Commits for new branch:")
				for _, c := range groupReadable {
					splog.Info("  %s", c)
				}
				splog.Info("")
				name, err = promptBranchName(existingNames, originalBranch, len(groups)+1, eng)
				if err != nil {
					return nil, err
				}
			}
			existingNames = append(existingNames, name)

			if groupSize == 1 {
				splog.Info("New branch: %s", style.ColorBranchName(name, false))
				splog.Info("  %s", groupReadable[0])
				splog.Info("")
			}

			groups = append(groups, branchGroup{
				startIdx: offset,
				endIdx:   offset + groupSize,
				name:     name,
			})
			offset += groupSize
		}
	}

	return groups, nil
}

// selectGroupSize prompts user to select how many commits to include in the next branch.
// Returns the number of commits (1 to len(commits)).
func selectGroupSize(readableCommits []string) (int, error) {
	if !utils.IsInteractive() {
		return len(readableCommits), nil // In non-interactive mode, group all remaining
	}

	// Build choices similar to selectSplitPoint
	choices := []string{}
	sizeByIndex := make(map[int]int) // choice index -> group size

	// "All remaining" option at top
	allLine := "─────── all remaining as one branch ───────"
	choices = append(choices, allLine)
	sizeByIndex[0] = len(readableCommits)

	// Add commits and split lines
	for i, commit := range readableCommits {
		choices = append(choices, fmt.Sprintf("  %s", commit))
		if i < len(readableCommits)-1 {
			splitLine := "─────── include above ───────"
			choices = append(choices, splitLine)
			sizeByIndex[len(choices)-1] = i + 1 // Include commits 0..i
		}
	}

	// Build options for tui.PromptSelect
	var options []tui.SelectOption
	for i, choice := range choices {
		if size, ok := sizeByIndex[i]; ok {
			// This is a split line - selectable
			options = append(options, tui.SelectOption{
				Label: choice,
				Value: fmt.Sprintf("%d", size),
			})
		}
		// Skip commit lines - they're not selectable options
	}

	selected, err := tui.PromptSelect("Select commits for the next new branch:", options, 0)
	if err != nil {
		return 0, err
	}

	// Parse the group size from the selected value
	var size int
	if _, err := fmt.Sscanf(selected, "%d", &size); err != nil {
		return 0, fmt.Errorf("invalid selection")
	}

	return size, nil
}

// deriveBranchName generates a branch name from a commit subject using the branch pattern.
func deriveBranchName(ctx *app.Context, commitSubject string, pattern config.BranchPattern, existingNames []string, eng splitByCommitEngine) (string, error) {
	name, err := pattern.GetBranchName(ctx, commitSubject, "")
	if err != nil {
		return "", fmt.Errorf("failed to derive branch name: %w", err)
	}

	// Check for conflicts with existing names
	originalName := name
	suffix := 1
	for {
		conflict := slices.Contains(existingNames, name)
		// Also check existing branches in the repo
		if !conflict && eng.BranchNames().Contains(name) {
			conflict = true
		}
		if !conflict {
			break
		}
		suffix++
		name = fmt.Sprintf("%s-%d", originalName, suffix)
	}

	return name, nil
}
