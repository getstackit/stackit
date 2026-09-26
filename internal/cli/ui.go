package cli

import (
	"github.com/spf13/cobra"

	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/cli/common"
	"github.com/getstackit/stackit/internal/cli/dashboard"
)

// newUICmd creates the ui command
func newUICmd() *cobra.Command {
	var (
		runLocalCI bool
		stackOnly  bool
	)

	cmd := &cobra.Command{
		Use:   "ui",
		Short: "Open the live stack companion panel",
		Long: `Open a live, local-first companion panel for watching stacked work.

The panel automatically reloads when refs change and periodically refreshes
working-tree state. Press s to open the existing shipping dashboard.`,
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			opts := common.GetGlobalOptions(cmd)
			ctx, err := app.GetContext(cmd.Context(), opts)
			if err != nil {
				return err
			}

			return dashboard.RunCompanion(ctx, dashboard.CompanionOptions{
				RunLocalCI: runLocalCI,
				StackOnly:  stackOnly,
			})
		},
	}

	cmd.Flags().BoolVar(&runLocalCI, "local-ci", false, "Run local CI validation when analyzing combinations")
	cmd.Flags().BoolVarP(&stackOnly, "stack", "s", false, "Show only the current stack")

	return cmd
}
