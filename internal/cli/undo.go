package cli

import (
	"github.com/spf13/cobra"

	"stackit.dev/stackit/internal/actions/undo"
	"stackit.dev/stackit/internal/app"
	"stackit.dev/stackit/internal/cli/common"
)

// newUndoCmd creates the undo command
func newUndoCmd() *cobra.Command {
	var (
		snapshotID string
		force      bool
	)

	cmd := &cobra.Command{
		Use:   "undo",
		Short: "Restore the repository to a previous state",
		Long: `Restore the repository to a previous state before a Stackit command was executed.

This command shows an interactive list of available undo points. Each undo point
represents the state of the repository before a modifying Stackit command (like
'move', 'create', 'restack', etc.) was executed.

If you specify a snapshot ID with --snapshot, it will restore to that specific
state without prompting.`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return common.Run(cmd, func(ctx *app.Context) error {
				// Create runner (manages terminal state) and handler (processes events)
				runner, handler := NewUndoUI(ctx.Output, ctx.Logger)
				if runner != nil {
					defer runner.Cleanup()
				}

				// Run undo action
				return undo.Action(ctx, undo.Options{
					SnapshotID: snapshotID,
					Force:      force,
				}, handler)
			})
		},
	}

	// Add flags
	cmd.Flags().StringVar(&snapshotID, "snapshot", "", "Specific snapshot ID to restore (skips interactive selection)")
	cmd.Flags().BoolVarP(&force, "yes", "y", false, "Skip confirmation prompt")

	return cmd
}
