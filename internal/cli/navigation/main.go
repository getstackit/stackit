package navigation

import (
	"fmt"

	"github.com/spf13/cobra"

	"stackit.dev/stackit/internal/app"
	"stackit.dev/stackit/internal/cli/common"
	"stackit.dev/stackit/internal/tui/style"
)

// NewMainCmd creates the main command
func NewMainCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "main",
		Aliases: []string{"m"},
		Short:   "Switch to the main/trunk branch",
		Long: `Switch to the main/trunk branch.

Navigates to the configured trunk branch (typically "main" or "master").`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return common.Run(cmd, func(ctx *app.Context) error {
				trunk := ctx.Engine.Trunk()

				// Check if already on trunk
				current := ctx.Engine.CurrentBranch()
				if current != nil && current.GetName() == trunk.GetName() {
					ctx.Output.Info("Already on %s.", style.ColorBranchName(trunk.GetName(), true))
					return nil
				}

				// Checkout trunk
				if err := ctx.Engine.CheckoutBranch(ctx.Context, trunk); err != nil {
					return fmt.Errorf("failed to checkout %s: %w", trunk.GetName(), err)
				}

				ctx.Output.Info("Checked out %s.", style.ColorBranchName(trunk.GetName(), true))
				return nil
			})
		},
	}
	return cmd
}
