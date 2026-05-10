package stack

import (
	"github.com/spf13/cobra"

	"github.com/getstackit/stackit/internal/actions/flatten"
	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/cli/common"
)

// NewFlattenCmd creates the flatten command
func NewFlattenCmd() *cobra.Command {
	var yes bool

	cmd := &cobra.Command{
		Use:   "flatten [branch]",
		Short: "Flatten the stack by moving branches closer to trunk where possible",
		Long: `Flatten the stack by re-parenting branches as close to trunk as possible.

The command analyzes each branch in the stack and tests whether it can be
rebased directly onto trunk (or an intermediate branch closer to trunk).
Branches that depend on changes from their parent will stay in place.

This is useful after landing PRs from the middle of a stack, or when you
have independent changes that were developed as a chain but don't actually
depend on each other.

Examples:
  stackit flatten           # Flatten stack from current branch
  stackit flatten feature   # Flatten stack from the 'feature' branch
  stackit flatten --yes     # Skip confirmation prompt`,
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return common.Run(cmd, func(ctx *app.Context) error {
				// Determine target branch from positional argument
				var targetBranch string
				if len(args) > 0 {
					targetBranch = args[0]
				}

				opts := flatten.Options{
					BranchName:  targetBranch,
					SkipConfirm: yes,
				}

				// Pre-flight: detect "nothing to do" cases BEFORE starting the
				// TUI runner. Otherwise the runner sets output to quiet, the
				// "no branches" message gets suppressed, and the user only
				// sees bubbletea startup/teardown escape codes flash.
				hasWork, err := flatten.HasFlattenWork(ctx, opts)
				if err != nil {
					return err
				}
				if !hasWork {
					ctx.Output.Info("No branches to flatten.")
					return nil
				}

				// Create runner and handler. runner.Cleanup is nil-safe so
				// no extra guard is needed for the non-TTY path.
				runner, handler := NewFlattenUI(ctx.Output, ctx.Logger)
				defer runner.Cleanup()

				// Run flatten action
				return flatten.Action(ctx, opts, handler)
			})
		},
	}

	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation prompt.")

	return cmd
}
