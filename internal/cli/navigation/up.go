package navigation

import (
	"fmt"
	"slices"

	"github.com/spf13/cobra"

	"github.com/getstackit/stackit/internal/actions"
	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/cli/common"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/errors"
	"github.com/getstackit/stackit/internal/tui"
	"github.com/getstackit/stackit/internal/tui/style"
	"github.com/getstackit/stackit/internal/utils"
)

// NewUpCmd creates the up command
func NewUpCmd() *cobra.Command {
	var (
		steps    int
		toBranch string
	)

	cmd := &cobra.Command{
		Use:   "up [steps]",
		Short: "Switch to the child of the current branch",
		Long: `Switch to the child of the current branch.

Navigates up the stack away from trunk by switching to a child branch.
By default, moves one level up. Use the --steps flag or pass a number
as an argument to move multiple levels at once.

If multiple children exist, you will be prompted to select one, unless
the --to flag is used to specify a target branch to navigate towards.`,
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return common.Run(cmd, func(ctx *app.Context) error {
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

				// Build StackGraph for efficient traversals
				graph := ctx.Engine.Graph(engine.SortStrategyAlphabetical)

				// Traverse up the specified number of steps
				targetBranch := currentBranch.GetName()
				for i := 0; i < steps; i++ {
					children := flattenThroughAnchors(graph.ChildBranches(ctx.Engine.GetBranch(targetBranch)), graph)
					if len(children) == 0 {
						if i == 0 {
							ctx.Output.Info("Already at the top of the stack.")
							return nil
						}
						ctx.Output.Info("Stopped at %s (no further children after %d step(s)).", style.ColorBranchName(targetBranch, false), i)
						break
					}

					var nextBranch string
					var err error
					if len(children) == 1 {
						nextBranch = children[0].GetName()
					} else {
						// Multiple children, decide which way to go
						if toBranch != "" {
							// Try to find the child that leads to toBranch
							var candidates []string
							for _, child := range children {
								upstack := graph.Range(child, engine.StackRange{RecursiveChildren: true})
								upstackNames := upstack.Names()
								if child.GetName() == toBranch || slices.Contains(upstackNames, toBranch) {
									candidates = append(candidates, child.GetName())
								}
							}

							switch len(candidates) {
							case 1:
								nextBranch = candidates[0]
							case 0:
								// --to is not a descendant of any child
								ctx.Output.Warn("Branch %s is not a descendant of %s.", style.ColorBranchName(toBranch, false), style.ColorBranchName(targetBranch, false))
								fallthrough
							default:
								// Still ambiguous even with --to (shouldn't happen in a tree)
								nextBranch, err = promptForChild(children.Names(), targetBranch)
								if err != nil {
									return err
								}
							}
						} else {
							nextBranch, err = promptForChild(children.Names(), targetBranch)
							if err != nil {
								return err
							}
						}
					}

					ctx.Output.Info("⮑  %s", nextBranch)
					targetBranch = nextBranch
				}

				// Check if we actually moved
				if targetBranch == currentBranch.GetName() {
					return nil
				}

				_, err = common.Checkout(ctx, actions.CheckoutOptions{BranchName: targetBranch}, nil)
				return err
			})
		},
	}

	// Add flags
	cmd.Flags().IntVarP(&steps, "steps", "n", 1, "The number of levels to traverse upstack.")
	cmd.Flags().StringVar(&toBranch, "to", "", "Target branch to navigate towards. When multiple children exist, selects the path leading to this branch.")

	_ = cmd.RegisterFlagCompletionFunc("to", common.CompleteBranches)

	return cmd
}

// flattenThroughAnchors replaces worktree anchor branches with their non-anchor children.
func flattenThroughAnchors(branches engine.Branches, graph *engine.StackGraph) engine.Branches {
	result := engine.Branches{}
	for _, b := range branches {
		if b.IsWorktreeAnchor() {
			grandchildren := graph.ChildBranches(b)
			result = result.Concat(flattenThroughAnchors(grandchildren, graph))
		} else {
			result = result.Append(b)
		}
	}
	return result
}

func promptForChild(children []string, parent string) (string, error) {
	if !utils.IsInteractive() {
		return "", fmt.Errorf("multiple children found for %s; use --to or move in interactive mode", parent)
	}

	options := make([]tui.SelectOption, len(children))
	for i, child := range children {
		options[i] = tui.SelectOption{
			Label: child,
			Value: child,
		}
	}

	selected, err := tui.PromptSelect(fmt.Sprintf("Multiple children found for %s. Select one to move up:", parent), options, 0)
	if err != nil {
		return "", err
	}

	return selected, nil
}
