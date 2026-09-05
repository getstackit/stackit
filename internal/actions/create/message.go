package create

import (
	"fmt"

	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/editor"
	"github.com/getstackit/stackit/internal/utils"
)

func getCommitMessage(ctx *app.Context) (string, error) {
	template, err := ctx.Engine.GetCommitTemplate(ctx.Context)
	if err != nil {
		return "", err
	}

	msg, err := editor.Open(template, "COMMIT_EDITMSG-*")
	if err != nil {
		return "", err
	}

	return utils.CleanCommitMessage(msg), nil
}

// getCommitMessageForBranch gets the commit message, reading from stdin if needed.
// If branch name is not provided and commit message is empty, it will prompt for one in interactive mode.
func getCommitMessageForBranch(ctx *app.Context, opts *Options, commitMessage string) (string, error) {
	// If commit message is already provided, use it
	if commitMessage != "" {
		return commitMessage, nil
	}

	// Try reading from stdin first (even in non-interactive mode)
	stdinMsg, err := utils.ReadFromStdin()
	if err == nil && stdinMsg != "" {
		return stdinMsg, nil
	}

	// If branch name is provided, we can create the branch without a message
	// (will result in empty commit if there are staged changes)
	if opts.BranchName != "" {
		if !utils.IsInteractive() {
			return "", nil // Allow empty message when branch name is explicit
		}
		// Interactive: prompt for commit message via editor
		msg, err := getCommitMessage(ctx)
		if err != nil {
			return "", err
		}
		return msg, nil
	}

	// No branch name and no message - need one for branch generation
	if !utils.IsInteractive() {
		return "", fmt.Errorf("must specify either a branch name or commit message")
	}

	// Interactive: get commit message from editor
	msg, err := getCommitMessage(ctx)
	if err != nil {
		return "", err
	}
	if msg == "" {
		return "", fmt.Errorf("aborting due to empty commit message")
	}
	return msg, nil
}
