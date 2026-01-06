package stack

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"stackit.dev/stackit/internal/actions/combine"
	"stackit.dev/stackit/internal/app"
	"stackit.dev/stackit/internal/cli/common"
)

// NewCombineCmd creates the combine command
func NewCombineCmd() *cobra.Command {
	var (
		stacks []string
		dryRun bool
		force  bool
		wait   bool
		skipCI bool
		yes    bool
	)

	cmd := &cobra.Command{
		Use:   "combine",
		Short: "Combine multiple stacks into a single PR",
		Long: `Combine multiple independent stacks into a single consolidated PR.

This command allows you to select multiple stacks (each rooted at trunk) and merge
them together into a single PR. It handles:

  - Conflict detection: If a stack conflicts with others, it's skipped
  - Local CI validation: Runs a configured command to validate the combined code
  - Binary search: If CI fails, finds the largest subset that passes

Configuration:
  Set the CI command with: stackit config set combine.ciCommand "your-command"
  Set the CI timeout with: stackit config set combine.ciTimeout 600

Examples:
  stackit combine                          # Interactive stack selection
  stackit combine --stacks feat/a,feat/b   # Specify stacks directly
  stackit combine --skip-ci                # Skip local CI validation
  stackit combine --wait                   # Wait for remote CI and auto-merge`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return common.Run(cmd, func(ctx *app.Context) error {
				// Parse comma-separated stacks if provided as a single string
				var selectedStacks []string
				for _, s := range stacks {
					for _, part := range strings.Split(s, ",") {
						if trimmed := strings.TrimSpace(part); trimmed != "" {
							selectedStacks = append(selectedStacks, trimmed)
						}
					}
				}

				opts := combine.Options{
					SelectedStacks: selectedStacks,
					DryRun:         dryRun,
					Force:          force,
					Wait:           wait,
					SkipCI:         skipCI,
					Yes:            yes,
				}

				return runCombine(ctx, opts)
			})
		},
	}

	cmd.Flags().StringSliceVar(&stacks, "stacks", nil, "Stack root branches to combine (comma-separated, skips picker)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show what would be combined without executing")
	cmd.Flags().BoolVar(&force, "force", false, "Skip validation checks")
	cmd.Flags().BoolVar(&wait, "wait", false, "Wait for CI and auto-merge the combined PR")
	cmd.Flags().BoolVar(&skipCI, "skip-ci", false, "Skip local CI validation")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation prompts")

	return cmd
}

func runCombine(ctx *app.Context, opts combine.Options) error {
	// If no stacks specified and interactive mode, show picker
	if len(opts.SelectedStacks) == 0 && ctx.Interactive && !opts.Yes {
		stacks, err := combine.DiscoverStacks(ctx.Engine)
		if err != nil {
			return fmt.Errorf("failed to discover stacks: %w", err)
		}

		if len(stacks) == 0 {
			ctx.Output.Warn("No independent stacks found rooted at trunk")
			return nil
		}

		selected := promptStackSelection(ctx, stacks)
		if len(selected) == 0 {
			ctx.Output.Info("No stacks selected")
			return nil
		}
		opts.SelectedStacks = selected
	}

	// Execute combine action
	result, err := combine.Action(ctx, opts)
	if err != nil {
		return err
	}

	// Show final result
	if result.PRNumber > 0 {
		ctx.Output.Success("Created PR #%d: %s", result.PRNumber, result.PRURL)
	}

	return nil
}

// promptStackSelection shows an interactive multi-select for stack roots
func promptStackSelection(ctx *app.Context, stacks []combine.StackInfo) []string {
	if len(stacks) == 0 {
		return nil
	}

	// Build options for multi-select
	options := make([]string, len(stacks))
	for i, stack := range stacks {
		label := stack.RootBranch
		if stack.PRCount > 0 {
			label = fmt.Sprintf("%s (%d PRs)", stack.RootBranch, stack.PRCount)
		}
		if stack.Scope != "" {
			label = fmt.Sprintf("%s [%s]", label, stack.Scope)
		}
		options[i] = label
	}

	ctx.Output.Info("Available stacks (select with space, confirm with enter):")
	for _, opt := range options {
		ctx.Output.Info("  [ ] %s", opt)
	}

	// TODO: Use survey.MultiSelect for actual interactive selection
	// For now, return all stacks if in interactive mode
	ctx.Output.Warn("Interactive picker not yet implemented - selecting all stacks")
	selected := make([]string, len(stacks))
	for i, stack := range stacks {
		selected[i] = stack.RootBranch
	}
	return selected
}
