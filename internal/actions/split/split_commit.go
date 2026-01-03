package split

import (
	"context"
	"fmt"

	"github.com/AlecAivazis/survey/v2"

	"stackit.dev/stackit/internal/actions"
	"stackit.dev/stackit/internal/engine"
	"stackit.dev/stackit/internal/tui"
	"stackit.dev/stackit/internal/tui/style"
	"stackit.dev/stackit/internal/utils"
)

// splitByCommitEngine is a minimal interface needed for splitting by commit
type splitByCommitEngine interface {
	engine.BranchReader
	engine.PRManager
	engine.StackRewriter
}

// splitByCommit splits a branch by selecting commit points.
//
// Algorithm:
//  1. Get all commits on the branch in a readable format.
//  2. Interactively prompt the user to select split points (commits that will start a new branch).
//  3. Interactively prompt for a name for each new branch.
//  4. Detach HEAD to the original branch's head to prepare for state changes.
//  5. Return the selected branch names and points to be applied by the engine.
func splitByCommit(ctx context.Context, branchToSplit string, eng splitByCommitEngine, splog *tui.Splog) (*Result, error) {
	// Get readable commits
	branchToSplitObj := eng.GetBranch(branchToSplit)
	readableCommits, err := branchToSplitObj.GetAllCommits(engine.CommitFormatReadable)
	if err != nil {
		return nil, fmt.Errorf("failed to get commits: %w", err)
	}

	if len(readableCommits) == 0 {
		return nil, fmt.Errorf("no commits to split")
	}

	parentBranchName := branchToSplitObj.GetParentPrecondition()
	numChildren := len(branchToSplitObj.GetChildren())

	// Show instructions
	splog.Info("Splitting the commits of %s into multiple branches.", style.ColorBranchName(branchToSplit, true))
	branch := eng.GetBranch(branchToSplit)
	prInfo, _ := branch.GetPrInfo()
	if prInfo != nil && prInfo.Number() != nil {
		splog.Info("If any of the new branches keeps the name %s, it will be linked to PR #%d.",
			style.ColorBranchName(branchToSplit, true), *prInfo.Number())
	}
	splog.Info("")
	splog.Info("For each branch you'd like to create:")
	splog.Info("1. Choose which commit it begins at using the below prompt.")
	splog.Info("2. Choose its name.")
	splog.Info("")

	// Get branch points interactively
	branchPoints, err := getBranchPoints(readableCommits, numChildren, parentBranchName)
	if err != nil {
		return nil, err
	}

	// Get branch names
	branchNames := []string{}
	for i := 0; i < len(branchPoints); i++ {
		splog.Info("Commits for branch %d:", i+1)
		startIdx := branchPoints[len(branchPoints)-1-i]
		var endIdx int
		if i < len(branchPoints)-1 {
			endIdx = branchPoints[len(branchPoints)-2-i]
		} else {
			endIdx = len(readableCommits)
		}
		for j := startIdx; j < endIdx; j++ {
			splog.Info("  %s", readableCommits[j])
		}
		splog.Info("")

		branchName, err := promptBranchName(branchNames, branchToSplit, i+1, eng)
		if err != nil {
			return nil, err
		}
		branchNames = append(branchNames, branchName)
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

// getBranchPoints interactively gets branch points from the user
func getBranchPoints(readableCommits []string, numChildren int, parentBranchName string) ([]int, error) {
	if !utils.IsInteractive() {
		return nil, fmt.Errorf("branch points must be specified in non-interactive mode")
	}
	// Array where nth index is whether we want a branch pointing to nth commit
	isBranchPoint := make([]bool, len(readableCommits))
	isBranchPoint[0] = true // First commit always has a branch

	// Build choices for the prompt
	choices := []string{}
	if numChildren > 0 {
		choices = append(choices, fmt.Sprintf("%d %s", numChildren, actions.Pluralize("child", numChildren)))
	}

	// Add commits
	for i, commit := range readableCommits {
		status := " "
		if isBranchPoint[i] {
			status = "✓"
		}
		choices = append(choices, fmt.Sprintf("%s %s", status, commit))
	}

	// Add parent and confirm
	choices = append(choices, parentBranchName)
	choices = append(choices, "Confirm")

	// Interactive loop
	for {
		var selected string
		prompt := &survey.Select{
			Message: "Toggle a commit to split the branch there. Select confirm to finish.",
			Options: choices,
		}
		if err := survey.AskOne(prompt, &selected); err != nil {
			return nil, fmt.Errorf("canceled")
		}

		if selected == "Confirm" {
			break
		}

		// Find the index of the selected commit
		// Choices structure: [children line (if any), commits..., parent, confirm]
		for i, choice := range choices {
			if choice == selected {
				// Skip if it's the children line, parent, or confirm
				if i == 0 && numChildren > 0 {
					// Children line - skip
					continue
				}
				if i == len(choices)-2 {
					// Parent line - skip
					continue
				}
				if i == len(choices)-1 {
					// Confirm - already handled above
					continue
				}

				// Calculate commit index
				commitIdx := i
				if numChildren > 0 {
					commitIdx-- // Skip children line
				}

				if commitIdx >= 0 && commitIdx < len(readableCommits) {
					// Never toggle the first commit
					if commitIdx != 0 {
						isBranchPoint[commitIdx] = !isBranchPoint[commitIdx]
						// Update the choice display
						status := " "
						if isBranchPoint[commitIdx] {
							status = "✓"
						}
						choices[i] = fmt.Sprintf("%s %s", status, readableCommits[commitIdx])
					}
				}
				break
			}
		}
	}

	// Convert to array of indices
	branchPoints := []int{}
	for i, isPoint := range isBranchPoint {
		if isPoint {
			branchPoints = append(branchPoints, i)
		}
	}

	return branchPoints, nil
}
