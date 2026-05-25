package navigation

import (
	"github.com/spf13/cobra"

	"github.com/getstackit/stackit/internal/actions"
	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/cli/common"
	"github.com/getstackit/stackit/internal/engine"
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
			globalOpts := common.GetGlobalOptions(cmd)
			branchName := ""
			if len(args) > 0 {
				branchName = args[0]
			}
			if globalOpts.Quiet && (branchName != "" || trunk) {
				loadMode := engine.LoadModeBranchesOnly
				globalOpts.EngineLoadMode = &loadMode
			}

			return common.RunWithOptions(cmd, globalOpts, func(ctx *app.Context) error {
				// Create runner (manages terminal state) and handler (processes events)
				runner, handler := NewCheckoutUI(ctx.Output, ctx.Logger)
				if runner != nil {
					defer runner.Cleanup()
				}

				// Prepare options
				opts := actions.CheckoutOptions{
					BranchName:    branchName,
					ShowUntracked: showUntracked,
					All:           all,
					StackOnly:     stack,
					CheckoutTrunk: trunk,
				}

				// Execute checkout action with handler
				_, err := common.Checkout(ctx, opts, handler)
				if err != nil {
					return err
				}

				return nil
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
