package cli

import (
	"github.com/spf13/cobra"

	"github.com/getstackit/stackit/internal/actions/scope"
	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/cli/common"
	"github.com/getstackit/stackit/internal/cli/stack"
	"github.com/getstackit/stackit/internal/utils"
)

// newScopeCmd creates the scope command
func newScopeCmd() *cobra.Command {
	var (
		unset bool
		show  bool
	)

	cmd := &cobra.Command{
		Use:   "scope [name]",
		Short: "Manage the logical scope for the current branch",
		Long: `Manage the logical scope (e.g., Jira Ticket ID, Linear ID) for the current branch.
By default, branches inherit their scope from their parent. Using this command sets an
explicit override for the current branch and all its descendants.

To create a new branch with a scope, use 'stackit create --scope <name>'.

Use 'none' or 'clear' as the scope name to explicitly break the inheritance chain.`,
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return common.Run(cmd, func(ctx *app.Context) error {
				var scopeName string
				if len(args) > 0 {
					scopeName = args[0]
				}

				if scopeName == "" && !unset && !show {
					show = true // Default to show if no args/flags
				}

				opts := scope.Options{
					Scope: scopeName,
					Unset: unset,
					Show:  show,
				}

				handler := stack.NewScopeUI(ctx.Output, utils.IsInteractive())
				return scope.Action(ctx, opts, handler)
			})
		},
	}

	cmd.Flags().BoolVar(&unset, "unset", false, "Remove the explicit scope override from the current branch")
	cmd.Flags().BoolVar(&show, "show", false, "Show the current scope for this branch")

	return cmd
}
