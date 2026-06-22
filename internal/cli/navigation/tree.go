// Package navigation provides CLI commands for navigating branches in a stack.
package navigation

import (
	"github.com/spf13/cobra"

	"github.com/getstackit/stackit/internal/actions"
	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/cli/common"
	"github.com/getstackit/stackit/internal/errors"
)

// NewTreeCmd creates the tree command
func NewTreeCmd() *cobra.Command {
	f := &treeFlags{}

	cmd := &cobra.Command{
		Use:          "tree",
		Short:        "Display the branch tree: all branches tracked by Stackit, with dependencies and info for each",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return executeTree(cmd, f, actions.TreeStyleNormal)
		},
	}

	addTreeFlags(cmd, f)

	// Add subcommands
	cmd.AddCommand(newTreeFullCmd())
	cmd.AddCommand(newTreeShortCmd())

	return cmd
}

func newTreeFullCmd() *cobra.Command {
	f := &treeFlags{}
	cmd := &cobra.Command{
		Use:          "full",
		Short:        "Display the branch tree with GitHub state (PR status, CI checks)",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return executeTree(cmd, f, actions.TreeStyleFull)
		},
	}
	addTreeFlags(cmd, f)
	return cmd
}

func newTreeShortCmd() *cobra.Command {
	f := &treeFlags{}
	cmd := &cobra.Command{
		Use:          "short",
		Short:        "Display only the branch tree (no stats or PR info)",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return executeTree(cmd, f, actions.TreeStyleShort)
		},
	}
	addTreeFlags(cmd, f)
	return cmd
}

type treeFlags struct {
	stack         bool
	steps         int
	showUntracked bool
	interactive   bool
	showSHAs      bool
	jsonOutput    bool
}

func addTreeFlags(cmd *cobra.Command, f *treeFlags) {
	cmd.Flags().BoolVarP(&f.stack, "stack", "s", false, "Only show ancestors and descendants of the current branch")
	cmd.Flags().IntVarP(&f.steps, "steps", "n", 0, "Only show this many levels upstack and downstack. Implies --stack")
	cmd.Flags().BoolVarP(&f.showUntracked, "show-untracked", "u", false, "Include untracked branches in interactive selection")
	cmd.Flags().BoolVarP(&f.interactive, "interactive", "i", false, "Enable interactive mode with scrolling and collapsing")
	cmd.Flags().BoolVar(&f.showSHAs, "shas", false, "Show commit SHAs next to branch names (useful for debugging)")
	cmd.Flags().BoolVar(&f.jsonOutput, "json", false, "Output in JSON format")
}

func executeTree(cmd *cobra.Command, f *treeFlags, style string) error {
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
		opts := actions.TreeOptions{
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

		// Execute tree action
		return actions.TreeAction(ctx, opts)
	})
}
