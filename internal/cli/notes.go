package cli

import (
	"github.com/spf13/cobra"

	"github.com/getstackit/stackit/internal/actions/notes"
	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/cli/common"
	"github.com/getstackit/stackit/internal/git"
)

func newNotesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "notes",
		Short: "Manage contextual notes on commits",
		Long: `View and manage contextual notes stored as git notes on commits.

Notes can be organized by namespace (default: story). They survive
rebases and can be pushed/fetched alongside branches.

Examples:
	  stackit notes                        # Show note for HEAD
	  stackit notes show                   # Show note for HEAD
	  stackit notes show abc1234           # Show note for a specific commit
	  stackit notes show --namespace story # Show note in "story" namespace
	  stackit notes add -p "add logout"    # Add note to HEAD
	  stackit notes log                    # Show branch commits with notes
	  stackit notes push                   # Push notes to remote`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// Default: show note for HEAD
			return common.Run(cmd, func(ctx *app.Context) error {
				return notes.Show(ctx, notes.ShowOptions{Namespace: git.DefaultNotesNamespace})
			})
		},
	}

	cmd.AddCommand(newNotesShowCmd())
	cmd.AddCommand(newNotesAddCmd())
	cmd.AddCommand(newNotesLogCmd())
	cmd.AddCommand(newNotesPushCmd())

	return cmd
}

func newNotesShowCmd() *cobra.Command {
	namespace := git.DefaultNotesNamespace
	cmd := &cobra.Command{
		Use:   "show [commit]",
		Short: "Show a note for a commit",
		Long: `Show a namespaced note attached to a commit. Defaults to HEAD.

Examples:
	  stackit notes show                        # Show note for HEAD in story namespace
	  stackit notes show abc1234                # Show note for specific commit
	  stackit notes show --namespace learnings  # Show note in specific namespace`,
		SilenceUsage: true,
		Args:         cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return common.Run(cmd, func(ctx *app.Context) error {
				opts := notes.ShowOptions{Namespace: namespace}
				if len(args) > 0 {
					opts.Commit = args[0]
				}
				return notes.Show(ctx, opts)
			})
		},
	}
	cmd.Flags().StringVar(&namespace, "namespace", git.DefaultNotesNamespace, "Note namespace")
	return cmd
}

func newNotesAddCmd() *cobra.Command {
	var (
		namespace string
		prompt    string
		summary   string
		model     string
		memory    []string
		rawJSON   string
	)

	cmd := &cobra.Command{
		Use:   cmdAdd,
		Short: "Add a note to HEAD",
		Long: `Add or replace a namespaced note on the current commit (HEAD).

Notes can capture context such as rationale, story, or learnings.

Examples:
	  stackit notes add -p "add logout button" -s "Added LogoutButton component"
	  stackit notes add --namespace learnings -p "fix auth" -s "Fixed token refresh"
	  stackit notes add --json '{"prompt":"test","summary":"test summary"}'`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return common.Run(cmd, func(ctx *app.Context) error {
				return notes.Add(ctx, notes.AddOptions{
					Namespace: namespace,
					Prompt:    prompt,
					Summary:   summary,
					Model:     model,
					Memory:    memory,
					JSON:      rawJSON,
				})
			})
		},
	}

	cmd.Flags().StringVar(&namespace, "namespace", git.DefaultNotesNamespace, "Note namespace")
	cmd.Flags().StringVarP(&prompt, "prompt", "p", "", "Prompt or instruction context")
	cmd.Flags().StringVarP(&summary, "summary", "s", "", "Summary of what was done")
	cmd.Flags().StringVar(&model, "model", "", "Model used (optional)")
	cmd.Flags().StringArrayVar(&memory, "memory", nil, "Insight worth remembering (repeatable)")
	cmd.Flags().StringVar(&rawJSON, "json", "", "Raw JSON note payload (alternative to individual flags)")

	return cmd
}

func newNotesLogCmd() *cobra.Command {
	namespace := git.DefaultNotesNamespace
	cmd := &cobra.Command{
		Use:   "log",
		Short: "Show branch commits with notes",
		Long: `Show all commits on the current branch (relative to parent) with
notes from a namespace, if any.

Examples:
	  stackit notes log
	  stackit notes log --namespace learnings`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return common.Run(cmd, func(ctx *app.Context) error {
				return notes.Log(ctx, notes.LogOptions{Namespace: namespace})
			})
		},
	}
	cmd.Flags().StringVar(&namespace, "namespace", git.DefaultNotesNamespace, "Note namespace")
	return cmd
}

func newNotesPushCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "push",
		Short: "Push notes to the remote",
		Long: `Push all notes to the remote repository.

This is also done automatically during 'stackit submit'.

Examples:
  stackit notes push`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return common.Run(cmd, notes.Push)
		},
	}
}
