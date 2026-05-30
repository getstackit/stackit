package navigation

import (
	"github.com/spf13/cobra"

	"github.com/getstackit/stackit/internal/actions"
	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/cli/common"
)

// NewShareCmd creates the share command.
func NewShareCmd() *cobra.Command {
	var (
		branch string
		marker string
	)

	cmd := &cobra.Command{
		Use:   "share",
		Short: "Print the current stack as Slack-ready markdown for copy-paste",
		Long: `Render the current stack as Slack-flavored markdown (mrkdwn) that you can
copy and paste straight into a Slack message.

Each branch is listed base-first as a bullet. Branches with an open PR become a
clickable link (#<number> <title>); branches without a PR show their branch name
so the stack is still complete before you submit. The branch you're on is marked.

Examples:
  stackit share                      # Share the current stack
  stackit share --branch feature-x   # Share the stack containing feature-x
  stackit share --marker '(here)'    # Use a custom current-branch marker`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return common.Run(cmd, func(ctx *app.Context) error {
				return actions.ShareAction(ctx, actions.ShareOptions{
					BranchName: branch,
					Marker:     marker,
				})
			})
		},
	}

	cmd.Flags().StringVarP(&branch, "branch", "b", "", "Share the stack containing this branch (defaults to the current branch)")
	cmd.Flags().StringVar(&marker, "marker", "", "Marker appended to the current branch line (default \"👈\")")

	return cmd
}
