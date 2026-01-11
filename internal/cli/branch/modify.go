// Package branch provides CLI commands for managing branches in a stack.
package branch

import (
	"github.com/spf13/cobra"

	"stackit.dev/stackit/internal/actions"
	"stackit.dev/stackit/internal/app"
	"stackit.dev/stackit/internal/cli/common"
)

// NewModifyCmd creates the modify command
func NewModifyCmd() *cobra.Command {
	var (
		all               bool
		commit            bool
		edit              bool
		interactiveRebase bool
		message           string
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
  stackit modify -a                       # Stage all and amend (opens editor)
  stackit modify -p                       # Interactive patch staging then amend
  stackit modify -c -a -m "New commit"    # Create new commit instead of amending
  stackit modify --interactive-rebase     # Interactive rebase on branch commits`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return common.Run(cmd, func(ctx *app.Context) error {
				// Determine noEdit flag:
				// - If --no-edit is explicitly set, use it
				// - If message is provided, don't open editor (noEdit = true)
				// - If --edit is set, open editor (noEdit = false)
				// - Default: open editor when amending without message (noEdit = false)
				noEditFlag := noEdit
				if message != "" && !edit {
					noEditFlag = true
				}

				// Run modify action
				return actions.ModifyAction(ctx, actions.ModifyOptions{
					All:               all,
					Update:            update,
					Patch:             patch,
					CreateCommit:      commit,
					Message:           message,
					Edit:              edit,
					NoEdit:            noEditFlag,
					ResetAuthor:       resetAuthor,
					Verbose:           verbose,
					InteractiveRebase: interactiveRebase,
				})
			})
		},
	}

	// Add flags
	cmd.Flags().BoolVarP(&all, "all", "a", false, "Stage all changes before committing.")
	cmd.Flags().BoolVarP(&commit, "commit", "c", false, "Create a new commit instead of amending the current commit. If this branch has no commits, this command always creates a new commit.")
	cmd.Flags().BoolVarP(&edit, "edit", "e", false, "If passed, open an editor to edit the commit message.")
	cmd.Flags().BoolVar(&interactiveRebase, "interactive-rebase", false, "Ignore all other flags and start a git interactive rebase on the commits in this branch.")
	cmd.Flags().StringVarP(&message, "message", "m", "", "The message for the new or amended commit. If passed, no editor is opened.")
	cmd.Flags().BoolVarP(&noEdit, "no-edit", "n", false, "Don't modify the existing commit message. Takes precedence over --edit.")
	cmd.Flags().BoolVarP(&patch, "patch", "p", false, "Pick hunks to stage before committing.")
	cmd.Flags().BoolVar(&resetAuthor, "reset-author", false, "Set the author of the commit to the current user if amending.")
	cmd.Flags().BoolVarP(&update, "update", "u", false, "Stage all updates to tracked files before committing.")
	cmd.Flags().CountVarP(&verbose, "verbose", "v", "Show unified diff between the HEAD commit and what would be committed at the bottom of the commit message template.")

	return cmd
}
