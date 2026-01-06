package navigation

import (
	"github.com/spf13/cobra"

	"stackit.dev/stackit/internal/actions"
	"stackit.dev/stackit/internal/app"
	"stackit.dev/stackit/internal/cli/common"
)

// NewCheckoutCmd creates the checkout command
func NewCheckoutCmd() *cobra.Command {
	var (
		all           bool
		showUntracked bool
		stack         bool
		trunk         bool
	)

	cmd := &cobra.Command{
		Use:     "checkout [branch]",
		Aliases: []string{"co"},
		Short:   "Switch to a branch. If no branch is provided, opens an interactive selector.",
		Long: `Switch to a branch. If no branch is provided, opens an interactive selector.

The interactive selector allows you to navigate branches using arrow keys and filter
by typing. Use flags to customize which branches are shown.`,
		ValidArgsFunction: common.CompleteBranches,
		SilenceUsage:      true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return common.Run(cmd, func(ctx *app.Context) error {
				// Get branch name from args
				branchName := ""
				if len(args) > 0 {
					branchName = args[0]
				}

				// Create handler based on TTY availability
				handler := NewCheckoutHandler()

				// Prepare options
				opts := actions.CheckoutOptions{
					BranchName:    branchName,
					ShowUntracked: showUntracked,
					All:           all,
					StackOnly:     stack,
					CheckoutTrunk: trunk,
				}

				// Execute checkout action with handler
				return actions.CheckoutAction(ctx, opts, handler)
			})
		},
	}

	// Add flags
	cmd.Flags().BoolVarP(&all, "all", "a", false, "Show branches across all configured trunks in interactive selection")
	cmd.Flags().BoolVarP(&showUntracked, "show-untracked", "u", false, "Include untracked branches in interactive selection")
	cmd.Flags().BoolVarP(&stack, "stack", "s", false, "Only show ancestors and descendants of the current branch in interactive selection")
	cmd.Flags().BoolVarP(&trunk, "trunk", "t", false, "Checkout the current trunk")

	return cmd
}
