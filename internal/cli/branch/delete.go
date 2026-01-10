// Package branch provides CLI commands for managing branches in a stack.
package branch

import (
	"github.com/spf13/cobra"

	"stackit.dev/stackit/internal/actions/delete"
	"stackit.dev/stackit/internal/app"
	"stackit.dev/stackit/internal/cli/common"
)

// NewDeleteCmd creates the delete command
func NewDeleteCmd() *cobra.Command {
	var (
		downstack bool
		force     bool
		upstack   bool
	)

	cmd := &cobra.Command{
		Use:   "delete [name]",
		Short: "Delete a branch and its stackit metadata (local-only)",
		Long: `Delete a branch and its stackit metadata (local-only).

Children will be restacked onto the parent branch. If the branch is not merged
or closed, prompts for confirmation.

This command does not perform any action on GitHub or the remote repository.
If you delete a branch with an open pull request, you will need to manually
close the pull request.`,
		SilenceUsage:      true,
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: common.CompleteBranches,
		RunE: func(cmd *cobra.Command, args []string) error {
			return common.Run(cmd, func(ctx *app.Context) error {
				branchName := ""
				if len(args) > 0 {
					branchName = args[0]
				}

				// Create runner and handler
				runner, handler := NewDeleteUI(ctx.Output, ctx.Logger)
				if runner != nil {
					defer runner.Cleanup()
				}

				return delete.Action(ctx, delete.Options{
					BranchName: branchName,
					Downstack:  downstack,
					Force:      force,
					Upstack:    upstack,
				}, handler)
			})
		},
	}

	// Add flags
	cmd.Flags().BoolVar(&downstack, "downstack", false, "Also delete any ancestors of the specified branch.")
	cmd.Flags().BoolVarP(&force, "force", "f", false, "Delete the branch even if it is not merged or closed.")
	cmd.Flags().BoolVar(&upstack, "upstack", false, "Also delete any children of the specified branch.")

	return cmd
}
