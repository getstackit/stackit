// Package navigation provides CLI commands for navigating branches in a stack.
package navigation

import (
	"github.com/spf13/cobra"

	"stackit.dev/stackit/internal/actions"
	"stackit.dev/stackit/internal/app"
	"stackit.dev/stackit/internal/cli/common"
	"stackit.dev/stackit/internal/errors"
)

// NewLogCmd creates the log command
func NewLogCmd() *cobra.Command {
	f := &logFlags{}

	cmd := &cobra.Command{
		Use:          "log",
		Short:        "Log all branches tracked by Stackit, showing dependencies and info for each",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return executeLog(cmd, f, actions.LogStyleNormal)
		},
	}

	addLogFlags(cmd, f)

	// Add subcommands
	cmd.AddCommand(newLogFullCmd())
	cmd.AddCommand(newLogShortCmd())

	return cmd
}

func newLogFullCmd() *cobra.Command {
	f := &logFlags{}
	cmd := &cobra.Command{
		Use:          "full",
		Short:        "Log branches with GitHub state (PR status, CI checks)",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return executeLog(cmd, f, actions.LogStyleFull)
		},
	}
	addLogFlags(cmd, f)
	return cmd
}

func newLogShortCmd() *cobra.Command {
	f := &logFlags{}
	cmd := &cobra.Command{
		Use:          "short",
		Short:        "Log branches showing only the branch tree (no stats or PR info)",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return executeLog(cmd, f, actions.LogStyleShort)
		},
	}
	addLogFlags(cmd, f)
	return cmd
}

type logFlags struct {
	stack         bool
	steps         int
	showUntracked bool
	interactive   bool
	showSHAs      bool
	jsonOutput    bool
}

func addLogFlags(cmd *cobra.Command, f *logFlags) {
	cmd.Flags().BoolVarP(&f.stack, "stack", "s", false, "Only show ancestors and descendants of the current branch")
	cmd.Flags().IntVarP(&f.steps, "steps", "n", 0, "Only show this many levels upstack and downstack. Implies --stack")
	cmd.Flags().BoolVarP(&f.showUntracked, "show-untracked", "u", false, "Include untracked branches in interactive selection")
	cmd.Flags().BoolVarP(&f.interactive, "interactive", "i", false, "Enable interactive mode with scrolling and collapsing")
	cmd.Flags().BoolVar(&f.showSHAs, "shas", false, "Show commit SHAs next to branch names (useful for debugging)")
	cmd.Flags().BoolVar(&f.jsonOutput, "json", false, "Output in JSON format")
}

func executeLog(cmd *cobra.Command, f *logFlags, style string) error {
	return common.Run(cmd, func(ctx *app.Context) error {
		eng := ctx.Engine

		// Determine branch name
		trunk := eng.Trunk()
		branchName := trunk.GetName()
		if f.stack || f.steps > 0 {
			currentBranch := eng.CurrentBranch()
			if currentBranch == nil {
				return errors.ErrNotOnBranch
			}
			branchName = currentBranch.GetName()
		}

		// Prepare options
		opts := actions.LogOptions{
			Style:         style,
			BranchName:    branchName,
			ShowUntracked: f.showUntracked,
			Interactive:   f.interactive,
			ShowSHAs:      f.showSHAs,
			JSON:          f.jsonOutput,
		}

		if f.steps > 0 {
			opts.Steps = &f.steps
		}

		// Execute log action
		return actions.LogAction(ctx, opts)
	})
}
