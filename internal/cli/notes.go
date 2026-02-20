package cli

import (
	"github.com/spf13/cobra"

	"github.com/getstackit/stackit/internal/actions/notes"
	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/cli/common"
)

func newNotesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "notes",
		Short: "Manage prompt notes on commits",
		Long: `View and manage LLM prompt notes stored as git notes on commits.

Prompt notes track what was asked of an AI and what it did, providing
an audit trail for AI-assisted changes. Notes survive rebases and
can be pushed/fetched alongside branches.

Examples:
  stackit notes                        # Show note for HEAD
  stackit notes show                   # Show note for HEAD
  stackit notes show abc1234           # Show note for specific commit
  stackit notes add -p "add logout"    # Add note to HEAD
  stackit notes log                    # Show branch commits with notes
  stackit notes push                   # Push notes to remote`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// Default: show note for HEAD
			return common.Run(cmd, func(ctx *app.Context) error {
				return notes.Show(ctx, notes.ShowOptions{})
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
	return &cobra.Command{
		Use:   "show [commit]",
		Short: "Show the prompt note for a commit",
		Long: `Show the prompt note attached to a commit. Defaults to HEAD.

Examples:
  stackit notes show          # Show note for HEAD
  stackit notes show abc1234  # Show note for specific commit`,
		SilenceUsage: true,
		Args:         cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return common.Run(cmd, func(ctx *app.Context) error {
				opts := notes.ShowOptions{}
				if len(args) > 0 {
					opts.Commit = args[0]
				}
				return notes.Show(ctx, opts)
			})
		},
	}
}

func newNotesAddCmd() *cobra.Command {
	var (
		prompt  string
		summary string
		model   string
		memory  []string
		rawJSON string
	)

	cmd := &cobra.Command{
		Use:   "add",
		Short: "Add a prompt note to HEAD",
		Long: `Add or replace a prompt note on the current commit (HEAD).

Notes store LLM context: what was asked, what was done, which model,
and key insights worth remembering.

Examples:
  stackit notes add -p "add logout button" -s "Added LogoutButton component"
  stackit notes add -p "fix auth" -s "Fixed token refresh" --model claude-opus-4 --memory "uses Supabase"
  stackit notes add --json '{"prompt":"test","summary":"test summary"}'`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return common.Run(cmd, func(ctx *app.Context) error {
				return notes.Add(ctx, notes.AddOptions{
					Prompt:  prompt,
					Summary: summary,
					Model:   model,
					Memory:  memory,
					JSON:    rawJSON,
				})
			})
		},
	}

	cmd.Flags().StringVarP(&prompt, "prompt", "p", "", "The user's instruction to the LLM")
	cmd.Flags().StringVarP(&summary, "summary", "s", "", "What the AI actually did")
	cmd.Flags().StringVar(&model, "model", "", "Which model was used")
	cmd.Flags().StringArrayVar(&memory, "memory", nil, "Key insight worth remembering (repeatable)")
	cmd.Flags().StringVar(&rawJSON, "json", "", "Raw JSON note (alternative to individual flags)")

	return cmd
}

func newNotesLogCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "log",
		Short: "Show branch commits with their prompt notes",
		Long: `Show all commits on the current branch (relative to parent) with
their prompt notes, if any.

Examples:
  stackit notes log`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return common.Run(cmd, notes.Log)
		},
	}
}

func newNotesPushCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "push",
		Short: "Push prompt notes to the remote",
		Long: `Push all prompt notes to the remote repository.

This is also done automatically during 'stackit submit'.

Examples:
  stackit notes push`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return common.Run(cmd, notes.Push)
		},
	}
}
