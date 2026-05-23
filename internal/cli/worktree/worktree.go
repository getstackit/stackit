// Package worktree provides CLI commands for managing stackit-managed worktrees.
package worktree

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/getstackit/stackit/internal/actions/worktree"
	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/cli/common"
	"github.com/getstackit/stackit/internal/tui/style"
)

// NewWorktreeCmd creates the worktree command group
func NewWorktreeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "worktree",
		Aliases: []string{"wt"},
		Short:   "Manage stackit-managed worktrees",
		Long: `Manage stackit-managed worktrees.

Worktrees allow you to work on multiple stacks in parallel, each in its own
directory. Create a worktree with 'stackit worktree create' from trunk.`,
	}

	cmd.AddCommand(newCreateCmd())
	cmd.AddCommand(newListCmd())
	cmd.AddCommand(newRemoveCmd())
	cmd.AddCommand(newOpenCmd())
	cmd.AddCommand(newPruneCmd())
	cmd.AddCommand(newAttachCmd())
	cmd.AddCommand(newDetachCmd())

	return cmd
}

// newCreateCmd creates the worktree create command
func newCreateCmd() *cobra.Command {
	var (
		scope  string
		noOpen bool
	)

	cmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a new worktree",
		Long: `Create a new stackit-managed worktree.

Creates a worktree with an anchor branch that tracks trunk. The anchor branch
serves as the base for stacked branches created within the worktree.

The worktree will be created in a sibling directory to your repository.

With shell integration enabled, automatically changes to the new worktree directory.
Use --no-open to skip the automatic directory change.`,
		SilenceUsage: true,
		Args:         cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return common.Run(cmd, func(ctx *app.Context) error {
				result, err := worktree.CreateAction(ctx, worktree.CreateOptions{
					Name:  args[0],
					Scope: scope,
				})
				if err != nil {
					return err
				}

				// Auto-cd to worktree by default when shell integration is available
				if !noOpen && result.Path != "" && common.HasShellIntegration() {
					ctx.Output.DirectiveCD(result.Path)
				} else if result.Path != "" {
					// User opted out or no shell integration
					ctx.Output.Tip("cd %s", result.Path)
				}

				return nil
			})
		},
	}

	cmd.Flags().StringVarP(&scope, "scope", "s", "", "Scope to apply to all branches in this worktree")
	cmd.Flags().BoolVar(&noOpen, "no-open", false, "Don't change to the worktree directory after creation")

	return cmd
}

// newListCmd creates the worktree list command
func newListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all managed worktrees",
		Long: `List all stackit-managed worktrees.

Shows each worktree's name, stack size, current branch, and status.`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return common.Run(cmd, func(ctx *app.Context) error {
				result, err := worktree.ListAction(ctx, worktree.ListOptions{})
				if err != nil {
					return err
				}

				if len(result.Worktrees) == 0 {
					ctx.Output.Info("No managed worktrees found.")
					ctx.Output.Tip("Create one with: stackit worktree create <name>")
					return nil
				}

				for _, wt := range result.Worktrees {
					renderWorktreeEntry(ctx, wt, result.CurrentAnchor)
				}

				return nil
			})
		},
	}

	return cmd
}

// renderWorktreeEntry renders a single worktree entry
func renderWorktreeEntry(ctx *app.Context, wt worktree.Entry, currentAnchor string) {
	// Indicator for current worktree
	indicator := "  "
	isCurrent := currentAnchor != "" && wt.AnchorBranch == currentAnchor
	if isCurrent {
		indicator = style.ColorGreen("@ ")
	}

	// Name (use worktree name if available, otherwise anchor branch)
	name := wt.Name
	if name == "" {
		name = wt.AnchorBranch
	}
	coloredName := style.ColorBranchName(name, isCurrent)

	// Handle missing worktrees
	if !wt.Exists {
		ctx.Output.Println(fmt.Sprintf("%s%s  %s", indicator, coloredName, style.ColorRed("(missing)")))
		return
	}

	// Stack size
	stackInfo := style.ColorDim("empty")
	if wt.StackSize > 0 {
		branchWord := "branch"
		if wt.StackSize > 1 {
			branchWord = "branches"
		}
		stackInfo = fmt.Sprintf("%d %s", wt.StackSize, branchWord)
	}

	// Current branch in worktree (only show if different from anchor)
	branchInfo := ""
	if wt.CurrentBranch != "" && wt.CurrentBranch != wt.AnchorBranch {
		branchInfo = style.ColorCyan(wt.CurrentBranch)
	}

	// Status indicator
	status := ""
	if wt.IsDirty {
		status = style.ColorYellow("*")
	}

	// Build the output line
	line := fmt.Sprintf("%s%s  %s", indicator, coloredName, stackInfo)
	if branchInfo != "" {
		line += fmt.Sprintf("  on %s", branchInfo)
	}
	if status != "" {
		line += " " + status
	}

	ctx.Output.Println(line)
}

// newRemoveCmd creates the worktree remove command
func newRemoveCmd() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "remove <name-or-anchor-branch>",
		Short: "Remove a managed worktree",
		Long: `Remove a stackit-managed worktree.

This removes an empty worktree directory, unregisters it from stackit, and
deletes the hidden anchor branch. You can specify either the worktree name or
the anchor branch name.

Worktrees with real branches are refused. Use 'stackit worktree detach' to
remove the worktree while preserving those branches.`,
		SilenceUsage: true,
		Args:         cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return common.Run(cmd, func(ctx *app.Context) error {
				return worktree.RemoveAction(ctx, worktree.RemoveOptions{
					AnchorBranch: args[0],
					Force:        force,
				})
			})
		},
	}

	cmd.Flags().BoolVarP(&force, "force", "f", false, "Discard uncommitted worktree changes while removing")

	return cmd
}

// newPruneCmd creates the worktree prune command
func newPruneCmd() *cobra.Command {
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "prune",
		Short: "Remove empty worktrees",
		Long: `Remove all worktrees that have no stacked branches.

Empty worktrees are those with only the anchor branch and no work in progress.
Worktrees with uncommitted changes are skipped.`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return common.Run(cmd, func(ctx *app.Context) error {
				result, err := worktree.PruneAction(ctx, worktree.PruneOptions{
					DryRun: dryRun,
				})
				if err != nil {
					return err
				}

				if len(result.Pruned) == 0 && len(result.Skipped) == 0 {
					ctx.Output.Info("No empty worktrees to prune.")
					return nil
				}

				if dryRun {
					ctx.Output.Info("Would prune:")
				}

				for _, name := range result.Pruned {
					if dryRun {
						ctx.Output.Println(fmt.Sprintf("  %s", style.ColorBranchName(name, false)))
					} else {
						ctx.Output.Success("Pruned %s", style.ColorBranchName(name, false))
					}
				}

				for _, entry := range result.Skipped {
					ctx.Output.Info("Skipped %s: %s", style.ColorBranchName(entry.Name, false), style.ColorDim(entry.Reason))
				}

				return nil
			})
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show what would be pruned without removing")

	return cmd
}

// newOpenCmd creates the worktree open command
func newOpenCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "open <name-or-anchor-branch>",
		Short: "Open a worktree (with shell integration) or print its path",
		Long: `Open a stackit-managed worktree.

You can specify either the worktree name or the anchor branch name.

With shell integration enabled, this command will automatically change
your working directory to the worktree. Without shell integration, it
prints the path for use with cd:

  cd $(stackit worktree open my-feature)

To enable shell integration, add to your shell config:

  eval "$(stackit shell zsh)"   # or bash/fish`,
		SilenceUsage: true,
		Args:         cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return common.Run(cmd, func(ctx *app.Context) error {
				path, err := worktree.OpenAction(ctx, worktree.OpenOptions{
					AnchorBranch: args[0],
				})
				if err != nil {
					return err
				}

				// Always print the path for scripting compatibility (cd $(stackit worktree open foo))
				ctx.Output.Print(path)
				ctx.Output.Newline()

				// Also emit directive for shell integration auto-cd
				if common.HasShellIntegration() {
					ctx.Output.DirectiveCD(path)
				}
				return nil
			})
		},
	}

	return cmd
}

// newAttachCmd creates the worktree attach command
func newAttachCmd() *cobra.Command {
	var name string
	var noOpen bool

	cmd := &cobra.Command{
		Use:   "attach <branch>",
		Short: "Create a worktree for an existing stack",
		Long: `Create a worktree for an existing stack.

Takes any branch in the stack and creates a worktree for the entire stack.
Stackit inserts a hidden anchor above the stack root and checks the worktree
out on the original stack root branch.

Examples:
  stackit worktree attach feature      # Attach stack rooted at 'feature'
  stackit wt attach feature --name wt  # Use custom worktree name
  stackit wt attach child-branch       # Attach the stack containing 'child-branch'

With shell integration enabled, automatically changes to the new worktree directory.
Use --no-open to skip the automatic directory change.`,
		SilenceUsage: true,
		Args:         cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return common.Run(cmd, func(ctx *app.Context) error {
				result, err := worktree.AttachAction(ctx, worktree.AttachOptions{
					Branch: args[0],
					Name:   name,
				})
				if err != nil {
					return err
				}

				// Auto-cd to worktree by default when shell integration is available
				if !noOpen && result.Path != "" && common.HasShellIntegration() {
					ctx.Output.DirectiveCD(result.Path)
				} else if result.Path != "" {
					// User opted out or no shell integration
					ctx.Output.Tip("cd %s", result.Path)
				}

				return nil
			})
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "Custom name for the worktree (defaults to stack root branch name)")
	cmd.Flags().BoolVar(&noOpen, "no-open", false, "Don't change to the worktree directory after creation")

	return cmd
}

// newDetachCmd creates the worktree detach command
func newDetachCmd() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "detach <name-or-branch>",
		Short: "Remove a worktree while keeping stack branches",
		Long: `Remove a worktree while preserving all stack branches.

Unlike 'remove', this command preserves the branches in the main repository.
Use this when you want to stop working in a worktree but keep all your branches.

Detach removes the worktree, reparents children of the hidden anchor to the
anchor's parent, then deletes the anchor branch.

Examples:
  stackit worktree detach my-feature   # Detach by worktree name
  stackit wt detach feature-branch     # Detach by anchor branch name
  stackit wt detach my-wt --force      # Force detach with uncommitted changes`,
		SilenceUsage: true,
		Args:         cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return common.Run(cmd, func(ctx *app.Context) error {
				return worktree.DetachAction(ctx, worktree.DetachOptions{
					NameOrBranch: args[0],
					Force:        force,
				})
			})
		},
	}

	cmd.Flags().BoolVarP(&force, "force", "f", false, "Force detach even with uncommitted changes")

	return cmd
}
