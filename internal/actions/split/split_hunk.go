package split

import (
	"context"
	"fmt"
	"strings"

	"github.com/AlecAivazis/survey/v2"

	"stackit.dev/stackit/internal/engine"
	"stackit.dev/stackit/internal/git"
	"stackit.dev/stackit/internal/tui"
	"stackit.dev/stackit/internal/tui/style"
	"stackit.dev/stackit/internal/utils"
)

// splitByHunkEngine is a minimal interface needed for splitting by hunk
type splitByHunkEngine interface {
	engine.BranchReader
	engine.BranchWriter
	engine.PRManager
	engine.StackRewriter
}

// splitByHunk splits a branch by interactively staging hunks.
//
// Algorithm:
//  1. Detach HEAD and soft reset to the parent branch's tip, leaving changes unstaged.
//  2. Loop until no unstaged changes remain:
//     a. Show remaining unstaged changes.
//     b. Interactively prompt the user to stage hunks for the next branch.
//     c. Prompt for a commit message and branch name.
//     d. Create a new commit with the staged changes.
//  3. Return the created branch names.
func splitByHunk(ctx context.Context, branchToSplit engine.Branch, eng splitByHunkEngine, splog *tui.Splog) (*Result, error) {
	// Detach and reset branch changes
	if err := eng.DetachAndResetBranchChanges(ctx, branchToSplit.GetName()); err != nil {
		return nil, fmt.Errorf("failed to detach and reset: %w", err)
	}

	branchNames := []string{}

	// Get default commit message
	commitMessages, err := branchToSplit.GetAllCommits(engine.CommitFormatMessage)
	if err != nil {
		return nil, fmt.Errorf("failed to get commit messages: %w", err)
	}
	defaultCommitMessage := strings.Join(commitMessages, "\n\n")

	// Show instructions
	splog.Info("Splitting %s into multiple single-commit branches.", style.ColorBranchName(branchToSplit.GetName(), true))
	branch := eng.GetBranch(branchToSplit.GetName())
	prInfo, _ := branch.GetPrInfo()
	if prInfo != nil && prInfo.Number() != nil {
		splog.Info("If any of the new branches keeps the name %s, it will be linked to PR #%d.",
			style.ColorBranchName(branchToSplit.GetName(), true), *prInfo.Number())
	}
	splog.Info("")
	splog.Info("For each branch you'd like to create:")
	splog.Info("1. Follow the prompts to stage the changes that you'd like to include.")
	splog.Info("2. Enter a commit message.")
	splog.Info("3. Pick a branch name.")
	splog.Info("The command will continue until all changes have been added to a new branch.")
	splog.Info("")

	// Loop while there are unstaged changes
	for {
		hasUnstaged, err := eng.HasUnstagedChanges(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to check unstaged changes: %w", err)
		}
		if !hasUnstaged {
			break
		}

		// Show remaining changes
		unstagedDiff, err := eng.GetUnstagedDiff(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to get unstaged diff: %w", err)
		}
		splog.Info("Remaining changes:")
		splog.Info("  %s", strings.ReplaceAll(unstagedDiff, "\n", "\n  "))
		splog.Info("")

		splog.Info("Stage changes for branch %d:", len(branchNames)+1)

		// Stage patch interactively
		if err := eng.StagePatch(ctx); err != nil {
			// If user cancels, restore branch
			_ = eng.ForceCheckoutBranch(ctx, branchToSplit)
			return nil, fmt.Errorf("canceled: no new branches created")
		}

		// Check if anything was staged
		hasStaged, err := eng.HasStagedChanges(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to check staged changes: %w", err)
		}
		if !hasStaged {
			splog.Info("No changes staged, skipping this branch.")
			continue
		}

		// Commit with message
		commitMessage := defaultCommitMessage
		var editMessage bool
		if !utils.IsInteractive() {
			// In non-interactive mode, use default message
			editMessage = false
		} else {
			prompt := &survey.Confirm{
				Message: "Edit commit message?",
				Default: true,
			}
			if err := survey.AskOne(prompt, &editMessage); err != nil {
				// If user cancels, restore branch
				_ = eng.ForceCheckoutBranch(ctx, branchToSplit)
				return nil, fmt.Errorf("canceled")
			}
		}

		if editMessage {
			// Get message from user
			msg, err := tui.OpenEditor(defaultCommitMessage, "COMMIT_EDITMSG-*")
			if err != nil {
				// If user cancels, restore branch
				_ = eng.ForceCheckoutBranch(ctx, branchToSplit)
				return nil, err
			}
			commitMessage = utils.CleanCommitMessage(msg)
		}

		// Create commit
		if err := eng.CommitWithOptions(ctx, git.CommitOptions{
			Message:  commitMessage,
			NoVerify: true, // Split hunk commits are internal, hooks usually shouldn't run
		}); err != nil {
			// If user cancels, restore branch
			_ = eng.ForceCheckoutBranch(ctx, branchToSplit)
			return nil, fmt.Errorf("failed to create commit: %w", err)
		}

		// Get branch name
		branchName, err := promptBranchName(branchNames, branchToSplit.GetName(), len(branchNames)+1, eng)
		if err != nil {
			// If user cancels, restore branch
			_ = eng.ForceCheckoutBranch(ctx, branchToSplit)
			return nil, err
		}
		branchNames = append(branchNames, branchName)
	}

	// Update branchToSplit to point to the last commit we created.
	// This is necessary because ApplySplitToCommits will use this branch name
	// to resolve commit SHAs using GetCommitSHA(branchToSplit, offset).
	// Since we've been creating commits in detached HEAD on top of the parent,
	// we need the original branch name to now point to the tip of our new commits.
	if err := eng.UpdateBranchRef(ctx, branchToSplit.GetName(), "HEAD"); err != nil {
		return nil, fmt.Errorf("failed to update branch reference: %w", err)
	}

	return &Result{
		BranchNames:  branchNames,
		BranchPoints: makeRange(len(branchNames)), // Each branch is a single commit
	}, nil
}
