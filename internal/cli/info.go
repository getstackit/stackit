package cli

import (
	"github.com/spf13/cobra"

	"github.com/getstackit/stackit/internal/actions"
	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/cli/common"
)

// newInfoCmd creates the info command
func newInfoCmd() *cobra.Command {
	var (
		body  bool
		diff  bool
		patch bool
		stat  bool
		stack bool
		json  bool
	)

	cmd := &cobra.Command{
		Use:     "info [branch]",
		Short:   "Display information about the current branch or stack",
		Aliases: []string{"i"},
		Long: `Display information about a branch, including branch relationships,
PR status, and optionally diffs or patches.

If --stack is provided, displays information about all branches in the current stack.
If no branch is specified and --stack is not provided, displays information about the current branch.`,
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: common.CompleteBranches,
		SilenceUsage:      true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return common.Run(cmd, func(ctx *app.Context) error {
				branchName := ""
				if len(args) > 0 {
					branchName = args[0]
				}

				return actions.InfoAction(ctx, actions.InfoOptions{
					BranchName: branchName,
					Body:       body,
					Diff:       diff,
					Patch:      patch,
					Stat:       stat,
					Stack:      stack,
					JSON:       json,
				})
			})
		},
	}

	cmd.Flags().BoolVarP(&body, "body", "b", false, "Show the PR body, if it exists")
	cmd.Flags().BoolVarP(&diff, "diff", "d", false, "Show the diff between this branch and its parent. Takes precedence over patch")
	cmd.Flags().BoolVarP(&patch, "patch", "p", false, "Show the changes made by each commit")
	cmd.Flags().BoolVarP(&stat, "stat", "s", false, "Show a diffstat instead of a full diff. Modifies either --patch or --diff. If neither is passed, implies --diff")
	cmd.Flags().BoolVar(&stack, "stack", false, "Show information about the entire stack")
	cmd.Flags().BoolVar(&json, "json", false, "Output in JSON format")

	return cmd
}
