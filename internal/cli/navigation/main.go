package navigation

import (
	"github.com/spf13/cobra"

	"github.com/getstackit/stackit/internal/actions"
	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/cli/common"
)

// mainCmdName is the command name for NewMainCmd, matching the conventional
// trunk branch name it navigates to.
const mainCmdName = "main"

// NewMainCmd creates the main command
func NewMainCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     mainCmdName,
		Aliases: []string{"m"},
		Short:   "Switch to the main/trunk branch",
		Long: `Switch to the main/trunk branch.

Navigates to the configured trunk branch (typically "main" or "master").`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return common.Run(cmd, func(ctx *app.Context) error {
				_, err := common.Checkout(ctx, actions.CheckoutOptions{CheckoutTrunk: true}, nil)
				return err
			})
		},
	}
	return cmd
}
