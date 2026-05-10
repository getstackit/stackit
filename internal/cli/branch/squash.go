// Package branch provides CLI commands for managing branches in a stack.
package branch

import (
	"github.com/spf13/cobra"

	"github.com/getstackit/stackit/internal/actions"
	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/cli/common"
)

// NewSquashCmd creates the squash command
func NewSquashCmd() *cobra.Command {
	var (
		message     string
		messageFile string
		edit        bool
		noEdit      bool
	)

	cmd := &cobra.Command{
		Use:   "squash",
		Short: "Squash all commits in the current branch into a single commit and restack upstack branches",
		Long: `Squash all commits in the current branch into a single commit and restack upstack branches.

This command combines all commits in the current branch into a single commit. After squashing,
all upstack branches (children) are automatically restacked.

Examples:
  stackit squash -m "feat: combined work"     # Squash with inline message
  stackit squash -F /tmp/msg                  # Squash, reading message from a file
  stackit squash --no-edit                    # Squash, keeping the current message`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return common.Run(cmd, func(ctx *app.Context) error {
				resolvedMessage, err := common.ReadMessage(message, messageFile)
				if err != nil {
					return err
				}

				// Determine noEdit flag: noEdit = argv['no-edit'] || !argv.edit
				// If --no-edit is set, noEdit = true
				// If --edit is false, noEdit = true
				// Otherwise (edit is true, the default), noEdit = false (will edit)
				noEditFlag := noEdit || !edit

				// Run squash action
				return actions.SquashAction(ctx, actions.SquashOptions{
					Message: resolvedMessage,
					NoEdit:  noEditFlag,
				})
			})
		},
	}

	cmd.Flags().StringVarP(&message, "message", "m", "", "The updated message for the commit.")
	cmd.Flags().StringVarP(&messageFile, "message-file", "F", "", "Read commit message from a file (use \"-\" for stdin). Mutually exclusive with --message.")
	cmd.Flags().BoolVar(&edit, "edit", true, "Modify the existing commit message.")
	cmd.Flags().BoolVarP(&noEdit, "no-edit", "n", false, "Don't modify the existing commit message. Takes precedence over --edit")

	return cmd
}
