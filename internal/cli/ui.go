package cli

import (
	"github.com/spf13/cobra"

	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/cli/common"
	"github.com/getstackit/stackit/internal/cli/dashboard"
)

// newUICmd creates the ui command
func newUICmd() *cobra.Command {
	var runLocalCI bool

	cmd := &cobra.Command{
		Use:   "ui",
		Short: "Open the shippable work dashboard",
		Long: `Open an interactive dashboard focused on shipping work to trunk.

The dashboard shows all your stacks with their shippability status:
  ✓ Shippable - Ready to merge (approved, CI passing)
  ⏳ Pending - Waiting on CI or review
  ✗ Blocked - CI failed or changes requested
  ○ Incomplete - Missing PRs or drafts

Select stacks to ship together, analyze combinations, and create
consolidated PRs with a single action.`,
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			opts := common.GetGlobalOptions(cmd)
			ctx, err := app.GetContext(cmd.Context(), opts)
			if err != nil {
				return err
			}

			return dashboard.RunShippable(ctx, dashboard.ShippableOptions{
				RunLocalCI: runLocalCI,
			})
		},
	}

	cmd.Flags().BoolVar(&runLocalCI, "local-ci", false, "Run local CI validation when analyzing combinations")

	return cmd
}
