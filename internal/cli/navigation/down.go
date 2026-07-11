package navigation

import (
	"github.com/spf13/cobra"

	"github.com/getstackit/stackit/internal/actions"
	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/cli/common"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/errors"
	"github.com/getstackit/stackit/internal/tui/style"
)

// NewDownCmd creates the down command
func NewDownCmd() *cobra.Command {
	var (
		steps int
	)

	cmd := &cobra.Command{
		Use:   "down [steps]",
		Short: "Switch to the parent of the current branch",
		Long: `Switch to the parent of the current branch.

Navigates down the stack toward trunk by switching to the parent branch.
By default, moves one level down. Use the --steps flag or pass a number
as an argument to move multiple levels at once.`,
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			// down only walks the parent chain and checks out an exact branch. In
			// quiet mode no branch info is printed (which would build the full stack
			// graph), so the lighter branches-only load with lazy per-branch promotion
			// is sufficient — mirroring the quiet exact-checkout path in `co`. The
			// managed-worktree check is intentionally preserved (checkout relies on it
			// for worktree switching).
			opts := common.GetGlobalOptions(cmd)
			if opts.Quiet {
				loadMode := engine.LoadModeBranchesOnly
				opts.EngineLoadMode = &loadMode
			}
			return common.RunWithOptions(cmd, opts, func(ctx *app.Context) error {
				parsedSteps, err := parsePositiveSteps(args, steps)
				if err != nil {
					return err
				}
				steps = parsedSteps

				// Get current branch
				currentBranch := ctx.Engine.CurrentBranch()
				if currentBranch == nil {
					return errors.ErrNotOnBranch
				}

				// Check if on trunk
				if currentBranch.IsTrunk() {
					ctx.Output.Info("Already at trunk (%s).", style.ColorCurrentBranch(currentBranch.GetName()))
					return nil
				}

				// Traverse down the specified number of steps
				targetBranch := *currentBranch
				for i := 0; i < steps; i++ {
					parent := targetBranch.GetParent()
					// Skip worktree anchors transparently
					for parent != nil && parent.IsWorktreeAnchor() {
						parent = parent.GetParent()
					}
					if parent == nil {
						// No parent found - branch is untracked or we've gone past trunk
						if i == 0 {
							ctx.Output.Info("%s has no parent (untracked branch).", style.ColorCurrentBranch(currentBranch.GetName()))
							return nil
						}
						// We moved some steps but can't go further
						ctx.Output.Info("Stopped at %s (no further parent after %d step(s)).", style.ColorBranchName(targetBranch.GetName()), i)
						break
					}
					ctx.Output.Info("⮑  %s", parent.GetName())
					targetBranch = *parent
				}

				// Check if we actually moved
				if targetBranch.GetName() == currentBranch.GetName() {
					ctx.Output.Info("Already at the bottom of the stack.")
					return nil
				}

				_, err = common.Checkout(ctx, actions.CheckoutOptions{BranchName: targetBranch.GetName()}, nil)
				return err
			})
		},
	}

	// Add flags
	cmd.Flags().IntVarP(&steps, "steps", "n", 1, "The number of levels to traverse downstack.")

	return cmd
}
