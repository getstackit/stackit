// Package stack provides CLI commands for operating on entire stacks.
package stack

import (
	"github.com/spf13/cobra"

	"stackit.dev/stackit/internal/actions/sync"
	"stackit.dev/stackit/internal/app"
	"stackit.dev/stackit/internal/cli/common"
)

// NewSyncCmd creates the sync command
func NewSyncCmd() *cobra.Command {
	var (
		all     bool
		force   bool
		restack bool
		dryRun  bool
	)

	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Sync all branches with remote",
		Long: `Sync all branches with remote, prompting to delete any branches for PRs that have been merged or closed.
Restacks all branches in your repository that can be restacked without conflicts.
If trunk cannot be fast-forwarded to match remote, overwrites trunk with the remote version.`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return common.Run(cmd, func(ctx *app.Context) error {
				// Create runner (manages terminal state) and handler (processes events)
				runner, handler := NewSyncUI(ctx.Output, ctx.Logger)
				defer runner.Cleanup()

				// Run sync action with handler
				return sync.Action(ctx, sync.Options{
					All:     all,
					Force:   force,
					Restack: restack,
					DryRun:  dryRun,
				}, handler)
			})
		},
	}

	var noRestack bool

	cmd.Flags().BoolVarP(&all, "all", "a", false, "Sync branches across all configured trunks")
	cmd.Flags().BoolVarP(&force, "force", "f", false, "Don't prompt for confirmation before overwriting or deleting a branch")
	cmd.Flags().BoolVar(&restack, "restack", true, "Restack any branches that can be restacked without conflicts")
	cmd.Flags().BoolVar(&noRestack, "no-restack", false, "Skip restacking branches")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview metadata changes without applying them")

	// Apply --no-restack flag
	cmd.PreRun = func(_ *cobra.Command, _ []string) {
		if noRestack {
			restack = false
		}
	}

	return cmd
}
