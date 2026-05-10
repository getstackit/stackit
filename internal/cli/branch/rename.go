package branch

import (
	"github.com/spf13/cobra"

	"github.com/getstackit/stackit/internal/actions"
	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/cli/common"
)

// NewRenameCmd creates the rename command
func NewRenameCmd() *cobra.Command {
	var (
		force bool
	)

	cmd := &cobra.Command{
		Use:   "rename [name]",
		Short: "Rename the current branch and update metadata",
		Long: `Rename the current branch and update all stack metadata referencing it.

If no branch name is supplied, you will be prompted for a new branch name.
Note that this removes any association to a pull request, as GitHub pull request branch names are immutable.`,
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return common.Run(cmd, func(ctx *app.Context) error {
				newName := ""
				if len(args) > 0 {
					newName = args[0]
				}

				opts := actions.RenameOptions{
					NewName: newName,
					Force:   force,
				}

				return actions.RenameAction(ctx, opts)
			})
		},
	}

	cmd.Flags().BoolVarP(&force, "force", "f", false, "Allow renaming a branch that is already associated with an open GitHub pull request")

	return cmd
}
