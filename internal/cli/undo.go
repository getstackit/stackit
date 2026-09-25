package cli

import (
	"github.com/spf13/cobra"

	"github.com/getstackit/stackit/internal/actions/undo"
	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/cli/common"
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

Undo restores branches, metadata, and the working tree, including uncommitted
changes the undone command captured (for example, the edit 'modify' amended
into a commit). Because it resets the working tree, undo refuses to run while
you have tracked uncommitted changes of your own; commit or stash them first.

If you specify a snapshot ID with --snapshot, it will restore to that specific
state without prompting.

Examples:
  stackit undo                    # Pick an undo point interactively
  stackit undo --yes              # Skip the confirmation prompt
  stackit undo --snapshot <id>    # Restore a specific snapshot`,
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
