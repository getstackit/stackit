// Package worktree provides CLI commands for managing stackit-managed worktrees.
package worktree

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/spf13/cobra"

	"github.com/getstackit/stackit/internal/actions/worktree"
	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/cli/common"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/tui/style"
)

const generatedRunWorktreeFallbackName = "agent"

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
	cmd.AddCommand(newRunCmd())
	cmd.AddCommand(newAttachCmd())
	cmd.AddCommand(newDetachCmd())
	cmd.AddCommand(newRepairCmd())

	return cmd
}

// newRunCmd creates a worktree and runs a command inside it.
func newRunCmd() *cobra.Command {
	var (
		name  string
		scope string
	)

	cmd := &cobra.Command{
		Use:   "run -- <command> [args...]",
		Short: "Create a new worktree and run a command inside it",
		Long: `Create a new stackit-managed worktree with a generated name and run a command inside it.

The worktree starts on a hidden anchor branch at trunk. When the command creates
the first real branch with 'stackit create', that branch becomes rooted under
the worktree anchor.

Examples:
  stackit worktree run -- claude
  stackit wt run -- codex
  stackit wt run --name payments-agent -- claude`,
		SilenceUsage: true,
		Args:         cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			worktreeName := name
			if worktreeName == "" {
				worktreeName = generatedRunWorktreeName(args[0], time.Now())
			}

			return common.Run(cmd, func(ctx *app.Context) error {
				result, err := worktree.CreateAction(ctx, worktree.CreateOptions{
					Name:  worktreeName,
					Scope: scope,
				})
				if err != nil {
					return err
				}

				ctx.Output.Info("Running %s in %s", style.ColorCyan(args[0]), style.ColorDim(result.Path.String()))

				runCmd := exec.CommandContext(ctx.Context, args[0], args[1:]...)
				runCmd.Dir = result.Path.String()
				runCmd.Stdin = os.Stdin
				runCmd.Stdout = cmd.OutOrStdout()
				runCmd.Stderr = cmd.ErrOrStderr()

				if err := runCmd.Run(); err != nil {
					var exitErr *exec.ExitError
					if errors.As(err, &exitErr) {
						cmd.SilenceErrors = true
						return common.NewExitCodeError(exitErr.ExitCode(), err)
					}
					return fmt.Errorf("failed to run %s in worktree: %w", args[0], err)
				}
				return nil
			})
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "Use an explicit worktree name instead of generating one")
	cmd.Flags().StringVarP(&scope, "scope", "s", "", "Scope to apply to all branches in this worktree")

	return cmd
}

func generatedRunWorktreeName(command string, now time.Time) string {
	base := filepath.Base(strings.TrimSpace(command))
	if base == "." || base == string(os.PathSeparator) || base == "" {
		base = generatedRunWorktreeFallbackName
	}

	var sanitized strings.Builder
	lastHyphen := false
	for _, r := range strings.ToLower(base) {
		valid := unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_'
		if valid {
			sanitized.WriteRune(r)
			lastHyphen = false
			continue
		}
		if !lastHyphen {
			sanitized.WriteByte('-')
			lastHyphen = true
		}
	}

	prefix := strings.Trim(sanitized.String(), "-_")
	if prefix == "" {
		prefix = generatedRunWorktreeFallbackName
	}
	if len(prefix) > 32 {
		prefix = strings.Trim(prefix[:32], "-_")
	}
	if prefix == "" {
		prefix = generatedRunWorktreeFallbackName
	}

	return fmt.Sprintf("%s-%s", prefix, now.Format("20060102-150405-000000000"))
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
					ctx.Output.DirectiveCD(result.Path.String())
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
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all managed worktrees",
		Long: `List all stackit-managed worktrees.

Shows each worktree's name, root branch, stack size, registration health, and status.`,
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

				if jsonOutput {
					data, marshalErr := json.MarshalIndent(result, "", "  ")
					if marshalErr != nil {
						return fmt.Errorf("failed to marshal JSON: %w", marshalErr)
					}
					ctx.Output.Print(string(data))
					ctx.Output.Newline()
					return nil
				}

				needsRepair := false
				for _, wt := range result.Worktrees {
					renderWorktreeEntry(ctx, wt, result.CurrentAnchor)
					needsRepair = needsRepair || wt.Lifecycle.NeedsRepair()
				}
				for _, warning := range result.OwnershipWarnings {
					ctx.Output.Warn("Ownership warning: %s", warning)
				}
				if needsRepair {
					ctx.Output.Tip("Some managed worktrees need repair: stackit worktree repair")
				}

				return nil
			})
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

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
		name = engine.WorktreeName(wt.AnchorBranch)
	}
	coloredName := style.ColorBranchNameIf(name.String(), isCurrent)

	// Stack size
	stackInfo := style.ColorDim("empty")
	if wt.StackSize > 0 {
		branchWord := "branch"
		if wt.StackSize > 1 {
			branchWord = "branches"
		}
		stackInfo = fmt.Sprintf("%d %s", wt.StackSize, branchWord)
	}

	rootInfo := ""
	if len(wt.RootBranches) > 0 {
		rootInfo = fmt.Sprintf("root %s", style.ColorCyan(strings.Join(wt.RootBranches, ",")))
	}

	// Current branch in worktree (only show if different from anchor)
	branchInfo := ""
	if wt.CurrentBranch != "" && wt.CurrentBranch != wt.AnchorBranch {
		branchInfo = style.ColorCyan(wt.CurrentBranch)
	}

	// Status indicator
	statusParts := make([]string, 0, 4)
	if rootInfo != "" {
		statusParts = append(statusParts, rootInfo)
	}
	if !wt.Lifecycle.Exists() {
		statusParts = append(statusParts, style.ColorRed("missing"))
	}
	if wt.Lifecycle.IsDirty() {
		statusParts = append(statusParts, style.ColorYellow("dirty"))
	}
	switch {
	case wt.Lifecycle.NeedsRepair():
		statusParts = append(statusParts, style.ColorYellow("repair"))
	case wt.Lifecycle.CanRemove():
		statusParts = append(statusParts, style.ColorGreen("remove"))
	case wt.Lifecycle.CanDetach():
		statusParts = append(statusParts, style.ColorGreen("detach"))
	}
	if wt.Lifecycle.Registration != worktree.RegistrationStateOK || wt.StatusMessage != "" {
		statusParts = append(statusParts, style.ColorDim(wt.StatusMessage))
	}

	// Build the output line
	line := fmt.Sprintf("%s%s  %s", indicator, coloredName, stackInfo)
	if branchInfo != "" {
		line += fmt.Sprintf("  on %s", branchInfo)
	}
	if len(statusParts) > 0 {
		line += "  " + strings.Join(statusParts, "  ")
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
remove the worktree while preserving those branches.

The directory is deleted with everything in it, including files Git ignores —
warm-started .env files and caches have no copy in Git to recover from.`,
		SilenceUsage: true,
		Args:         cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return common.Run(cmd, func(ctx *app.Context) error {
				return worktree.RemoveAction(ctx, worktree.RemoveOptions{
					Selector: worktree.WorktreeSelector(args[0]),
					Policy:   worktree.RemovalPolicyForForce(force),
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
Worktrees with uncommitted changes are skipped.

Each directory is deleted with everything in it, including files Git ignores.
Ignored files do not count as uncommitted, so a worktree holding only a
warm-started .env is prunable and that file goes with it.`,
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
						ctx.Output.Println(fmt.Sprintf("  %s", style.ColorBranchName(name)))
					} else {
						ctx.Output.Success("Pruned %s", style.ColorBranchName(name))
					}
				}

				for _, entry := range result.Skipped {
					ctx.Output.Info("Skipped %s: %s", style.ColorBranchName(entry.Name), style.ColorDim(entry.Reason))
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
					Selector: worktree.WorktreeSelector(args[0]),
				})
				if err != nil {
					return err
				}

				// Always print the path for scripting compatibility (cd $(stackit worktree open foo))
				ctx.Output.Print(path.String())
				ctx.Output.Newline()

				// Also emit directive for shell integration auto-cd
				if common.HasShellIntegration() {
					ctx.Output.DirectiveCD(path.String())
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
					ctx.Output.DirectiveCD(result.Path.String())
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

What is preserved is your branches, not the directory: that is deleted with
everything in it, including files Git ignores. Copy out any warm-started .env
or cache you want to keep first.

Examples:
  stackit worktree detach my-feature   # Detach by worktree name
  stackit wt detach feature-branch     # Detach by anchor branch name
  stackit wt detach my-wt --force      # Force detach with uncommitted changes`,
		SilenceUsage: true,
		Args:         cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return common.Run(cmd, func(ctx *app.Context) error {
				return worktree.DetachAction(ctx, worktree.DetachOptions{
					Selector: worktree.WorktreeSelector(args[0]),
					Policy:   worktree.RemovalPolicyForForce(force),
				})
			})
		},
	}

	cmd.Flags().BoolVarP(&force, "force", "f", false, "Force detach even with uncommitted changes")

	return cmd
}

func newRepairCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "repair [name-or-anchor-branch]",
		Short: "Repair invalid or legacy managed worktree registrations",
		Long: `Repair invalid or legacy managed worktree registrations.

Without an argument, repairs every managed worktree registration that needs
attention. With a target, repairs only the named worktree or anchor branch.

Legacy registrations that point at a real stack root are converted to hidden
worktree anchors. Stale registrations with missing directories are removed.`,
		SilenceUsage: true,
		Args:         cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return common.Run(cmd, func(ctx *app.Context) error {
				target := ""
				if len(args) > 0 {
					target = args[0]
				}

				result, err := worktree.RepairAction(ctx, worktree.RepairOptions{Selector: worktree.WorktreeSelector(target)})
				if err != nil {
					return err
				}

				for _, repaired := range result.Repaired {
					ctx.Output.Success("%s: %s", style.ColorBranchName(repaired.Name), repaired.Action)
				}
				for _, skipped := range result.Skipped {
					ctx.Output.Info("Skipped %s: %s", style.ColorBranchName(skipped.Name), style.ColorDim(skipped.Reason))
				}
				if len(result.Repaired) == 0 && len(result.Skipped) == 0 {
					ctx.Output.Info("No managed worktrees needed repair.")
				}
				return nil
			})
		},
	}

	return cmd
}
