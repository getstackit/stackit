package cli

import (
	"github.com/spf13/cobra"

	"stackit.dev/stackit/internal/actions"
	"stackit.dev/stackit/internal/app"
	"stackit.dev/stackit/internal/cli/common"
)

// newHealthCmd creates the health command
func newHealthCmd() *cobra.Command {
	var (
		jsonOutput bool
		quiet      bool
	)

	cmd := &cobra.Command{
		Use:   "health",
		Short: "Check the health status of your stack",
		Long: `Analyze the health of all tracked branches in your stack.

Reports on:
- Branches that need restacking
- CI status (passing, failing, pending)
- PR review status (draft, open, approved, merged)
- Branches that are falling behind trunk
- Recommendations for improving stack health

Examples:
  stackit health              # Show human-readable health report
  stackit health --json       # Output health report as JSON (for scripts/tools)
  stackit health --quiet      # Only output if there are issues`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return common.Run(cmd, func(ctx *app.Context) error {
				return actions.HealthAction(ctx, actions.HealthOptions{
					JSON:  jsonOutput,
					Quiet: quiet,
				})
			})
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output health report as JSON")
	cmd.Flags().BoolVar(&quiet, "quiet", false, "Only output if there are health issues")

	return cmd
}
