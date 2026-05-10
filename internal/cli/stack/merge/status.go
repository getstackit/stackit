// Package merge provides CLI commands for merging stacked PRs.
package merge

import (
	"github.com/spf13/cobra"

	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/cli/common"
	"github.com/getstackit/stackit/internal/shippable"
)

// NewStatusCmd creates the merge status subcommand.
// This command displays the shippability status of stacks without entering the merge wizard.
func NewStatusCmd() *cobra.Command {
	var showAll bool

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show shippability status of your stacks",
		Long: `Show which stacks are ready to merge, pending review, or blocked.

By default, shows only your own stacks. Use --all to see the entire team's work.

Examples:
  stackit merge status        # Show your mergeable work
  stackit merge status --all  # Show entire team's mergeable work`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return common.Run(cmd, func(ctx *app.Context) error {
				analyzer := shippable.NewAnalyzer(ctx.Engine, ctx.GitHub())
				analysisResult, err := analyzer.AnalyzeAll(ctx.Context)
				if err != nil {
					return err
				}

				// Filter by current user unless --all is specified
				if !showAll && ctx.GitHub() != nil {
					currentUser, userErr := ctx.GitHub().GetCurrentUser(ctx.Context)
					if userErr == nil && currentUser != "" {
						analysisResult = analysisResult.FilterByAuthor(currentUser)
					}
				}

				DisplayMergeStatus(ctx.Output, analysisResult)
				return nil
			})
		},
	}

	cmd.Flags().BoolVar(&showAll, "all", false, "Show all team members' stacks (default: your stacks only)")

	return cmd
}
