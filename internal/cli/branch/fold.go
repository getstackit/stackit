// Package branch provides CLI commands for managing branches in a stack.
package branch

import (
	"github.com/spf13/cobra"

	"stackit.dev/stackit/internal/actions/fold"
	"stackit.dev/stackit/internal/app"
	"stackit.dev/stackit/internal/cli/common"
	_ "stackit.dev/stackit/internal/demo" // Register demo engine factory
)

// NewFoldCmd creates the fold command
func NewFoldCmd() *cobra.Command {
	var (
		keep       bool
		allowTrunk bool
		dryRun     bool
	)

	cmd := &cobra.Command{
		Use:   "fold",
		Short: "Fold a branch's changes into its parent",
		Long: `Fold a branch's changes into its parent, update dependencies of descendants
of the new combined branch, and restack.

This is useful when you have a branch that is no longer needed and you want to
combine its changes with its parent branch.

If the parent of the current branch is the trunk (e.g., main), you must provide
the --allow-trunk flag, as this will modify your local trunk branch directly.

This command does not perform any action on GitHub or the remote repository.
If you fold a branch with an open pull request, you will need to manually
close the pull request.`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return common.Run(cmd, func(ctx *app.Context) error {
				// Create runner and handler
				runner, handler := NewFoldUI(ctx.Output, ctx.Logger)
				if runner != nil {
					defer runner.Cleanup()
				}

				// Run fold action
				return fold.Action(ctx, fold.Options{
					Keep:       keep,
					AllowTrunk: allowTrunk,
					DryRun:     dryRun,
				}, handler)
			})
		},
	}

	// Add flags
	cmd.Flags().BoolVarP(&keep, "keep", "k", false, "Keeps the name of the current branch instead of using the name of its parent.")
	cmd.Flags().BoolVar(&allowTrunk, "allow-trunk", false, "Allows folding into the trunk branch (e.g., main).")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Shows what would happen without applying any changes.")

	return cmd
}
