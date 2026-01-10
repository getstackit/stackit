package branch

import (
	"github.com/spf13/cobra"

	"stackit.dev/stackit/internal/actions"
	"stackit.dev/stackit/internal/app"
	"stackit.dev/stackit/internal/cli/common"
)

// NewGetCmd creates the get command
func NewGetCmd() *cobra.Command {
	var (
		downstack bool
		force     bool
		restack   bool
		unfrozen  bool
	)

	cmd := &cobra.Command{
		Use:   "get [branch|PR]",
		Short: "Sync branches from trunk to the given branch from remote",
		Long: `Sync branches from trunk to the given branch from remote, prompting the user to resolve any conflicts.

If the branch passed to get already exists locally, any local branches upstack of the branch are also synced; 
to opt out of this behavior, use the --downstack flag. 

Note that remote-only branches upstack of the branch are not currently synced. 

If no branch is provided, sync the current stack.`,
		SilenceUsage: true,
		Args:         cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return common.Run(cmd, func(ctx *app.Context) error {
				branchOrPR := ""
				if len(args) > 0 {
					branchOrPR = args[0]
				}

				// Create runner (manages terminal state) and handler (processes events)
				runner, handler := NewGetUI(ctx.Output, ctx.Logger)
				if runner != nil {
					defer runner.Cleanup()
				}

				return actions.GetAction(ctx, branchOrPR, actions.GetOptions{
					Downstack: downstack,
					Force:     force,
					Restack:   restack,
					Unfrozen:  unfrozen,
				}, handler)
			})
		},
	}

	var noRestack bool

	cmd.Flags().BoolVarP(&downstack, "downstack", "d", false, "When syncing a branch that already exists locally, don't sync upstack branches.")
	cmd.Flags().BoolVarP(&force, "force", "f", false, "Overwrite all fetched branches with remote source of truth")
	cmd.Flags().BoolVar(&restack, "restack", true, "Restack any branches in the stack that can be restacked without conflicts")
	cmd.Flags().BoolVar(&noRestack, "no-restack", false, "Skip restacking branches")
	cmd.Flags().BoolVarP(&unfrozen, "unfrozen", "U", false, "Checkout new branches as unfrozen (allow local edits)")

	// Apply --no-restack flag
	cmd.PreRun = func(_ *cobra.Command, _ []string) {
		if noRestack {
			restack = false
		}
	}

	return cmd
}
