// Package branch provides CLI commands for managing branches in a stack.
package branch

import (
	"github.com/spf13/cobra"

	"github.com/getstackit/stackit/internal/actions"
	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/cli/common"
)

// NewModifyCmd creates the modify command
func NewModifyCmd() *cobra.Command {
	var (
		all               bool
		commit            bool
		edit              bool
		interactiveRebase bool
		into              string
		message           string
		messageFile       string
		noEdit            bool
		patch             bool
		resetAuthor       bool
		update            bool
		verbose           int
	)

	cmd := &cobra.Command{
		Use:     "modify",
		Aliases: []string{},
		Short:   "Modify the current branch by amending its commit or creating a new commit",
		Long: `Modify the current branch by amending its commit or creating a new commit.

Automatically restacks descendants after the modification.

Examples:
  stackit modify -a -m "Updated feature"  # Stage all and amend with message
  stackit modify -a -F /tmp/msg           # Stage all and amend, reading message from a file
  stackit modify -a                       # Stage all and amend in place (keeps existing message)
  stackit modify -a -e                    # Stage all and amend (opens editor)
  stackit modify -p                       # Interactive patch staging then amend
  stackit modify -c -a -m "New commit"    # Create new commit instead of amending
  stackit modify --interactive-rebase     # Interactive rebase on branch commits
  stackit modify -a --into feature-a      # Amend a downstack ancestor without checking it out`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return common.Run(cmd, func(ctx *app.Context) error {
				resolvedMessage, err := common.ReadMessage(message, messageFile)
				if err != nil {
					return err
				}

				// --edit explicitly requests the editor; everything else amends in place.
				noEditFlag := noEdit || !edit

				// Run modify action
				return actions.ModifyAction(ctx, actions.ModifyOptions{
					All:               all,
					Update:            update,
					Patch:             patch,
					CreateCommit:      commit,
					Message:           resolvedMessage,
					Edit:              edit,
					NoEdit:            noEditFlag,
					ResetAuthor:       resetAuthor,
					Verbose:           verbose,
					InteractiveRebase: interactiveRebase,
					Into:              into,
				})
			})
		},
	}

	// Add flags
	cmd.Flags().BoolVarP(&all, "all", "a", false, "Stage all changes before committing.")
	cmd.Flags().BoolVarP(&commit, "commit", "c", false, "Create a new commit instead of amending the current commit. If this branch has no commits, this command always creates a new commit.")
	cmd.Flags().BoolVarP(&edit, "edit", "e", false, "Open an editor to edit the commit message.")
	cmd.Flags().BoolVar(&interactiveRebase, "interactive-rebase", false, "Ignore all other flags and start a git interactive rebase on the commits in this branch.")
	cmd.Flags().StringVar(&into, "into", "", "Modify a downstack ancestor branch instead of the current branch, without checking it out first.")
	cmd.Flags().StringVarP(&message, "message", "m", "", "The message for the new or amended commit. If passed, no editor is opened.")
	cmd.Flags().StringVarP(&messageFile, "message-file", "F", "", "Read commit message from a file (use \"-\" for stdin). Mutually exclusive with --message.")
	cmd.Flags().BoolVarP(&noEdit, "no-edit", "n", false, "Don't modify the existing commit message. Takes precedence over --edit.")
	cmd.Flags().BoolVarP(&patch, "patch", "p", false, "Pick hunks to stage before committing.")
	cmd.Flags().BoolVar(&resetAuthor, "reset-author", false, "Set the author of the commit to the current user if amending.")
	cmd.Flags().BoolVarP(&update, "update", "u", false, "Stage all updates to tracked files before committing.")
	cmd.Flags().CountVarP(&verbose, "verbose", "v", "Show unified diff between the HEAD commit and what would be committed at the bottom of the commit message template.")

	_ = cmd.RegisterFlagCompletionFunc("into", common.CompleteBranches)

	return cmd
}
