// Package stack provides CLI commands for operating on entire stacks.
package stack

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/getstackit/stackit/internal/actions/submit"
	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/cli/common"
	"github.com/getstackit/stackit/internal/config"
	_ "github.com/getstackit/stackit/internal/demo" // Register demo engine factory
	"github.com/getstackit/stackit/internal/engine"
)

type submitFlags struct {
	branch               string
	stack                bool
	force                bool
	dryRun               bool
	confirm              bool
	updateOnly           bool
	always               bool
	restack              bool
	draft                bool
	publish              bool
	edit                 bool
	editTitle            bool
	editDescription      bool
	noEdit               bool
	noEditTitle          bool
	noEditDescription    bool
	reviewers            string
	teamReviewers        string
	mergeWhenReady       bool
	rerequestReview      bool
	view                 bool
	web                  bool
	comment              string
	targetTrunk          string
	ignoreOutOfSyncTrunk bool
	cli                  bool
	noLabels             bool
	noAssignees          bool
	jsonOutput           bool
}

func addSubmitFlags(cmd *cobra.Command, f *submitFlags) {
	cmd.Flags().StringVar(&f.branch, "branch", "", "Which branch to run this command from. Defaults to the current branch.")
	cmd.Flags().BoolVarP(&f.stack, "stack", "s", false, "Submit descendants of the current branch in addition to its ancestors.")
	cmd.Flags().BoolVarP(&f.force, "force", "f", false, "Force push: overwrites the remote branch with your local branch. Otherwise defaults to --force-with-lease.")
	cmd.Flags().BoolVar(&f.dryRun, "dry-run", false, "Reports the PRs that would be submitted and terminates. No branches are restacked or pushed and no PRs are opened or updated.")
	cmd.Flags().BoolVarP(&f.confirm, "confirm", "c", false, "Reports the PRs that would be submitted and asks for confirmation before pushing branches and opening/updating PRs.")
	cmd.Flags().BoolVarP(&f.updateOnly, "update-only", "u", false, "Only push branches and update PRs for branches that already have PRs open.")
	cmd.Flags().BoolVar(&f.always, "always", false, "Always push updates, even if the branch has not changed.")
	cmd.Flags().BoolVar(&f.restack, "restack", false, "Restack branches before submitting.")
	cmd.Flags().BoolVarP(&f.draft, "draft", "d", false, "If set, all new PRs will be created in draft mode.")
	cmd.Flags().BoolVarP(&f.publish, "publish", "p", false, "If set, publishes all PRs being submitted.")
	cmd.Flags().BoolVarP(&f.edit, "edit", "e", false, "Input metadata for all PRs interactively.")
	cmd.Flags().BoolVar(&f.editTitle, "edit-title", false, "Input the PR title interactively.")
	cmd.Flags().BoolVar(&f.editDescription, "edit-description", false, "Input the PR description interactively.")
	cmd.Flags().BoolVarP(&f.noEdit, "no-edit", "n", false, "Don't edit any PR fields inline.")
	cmd.Flags().BoolVar(&f.noEditTitle, "no-edit-title", false, "Don't prompt for the PR title.")
	cmd.Flags().BoolVar(&f.noEditDescription, "no-edit-description", false, "Don't prompt for the PR description.")
	cmd.Flags().StringVar(&f.reviewers, "reviewers", "", "If set without an argument, prompt to manually set reviewers. Alternatively, accepts a comma separated string of reviewers.")
	cmd.Flags().StringVar(&f.teamReviewers, "team-reviewers", "", "Comma separated list of team slugs.")
	cmd.Flags().BoolVar(&f.mergeWhenReady, "merge-when-ready", false, "If set, marks all PRs being submitted as merge when ready.")
	cmd.Flags().BoolVar(&f.rerequestReview, "rerequest-review", false, "Rerequest review from current reviewers.")
	cmd.Flags().BoolVarP(&f.view, "view", "v", false, "Open the PR in your browser after submitting.")
	cmd.Flags().BoolVarP(&f.web, "web", "w", false, "Open a web browser to edit PR metadata.")
	cmd.Flags().StringVar(&f.comment, "comment", "", "Add a comment on the PR with the given message.")
	cmd.Flags().StringVarP(&f.targetTrunk, "target-trunk", "t", "", "Which trunk to open PRs against on remote.")
	cmd.Flags().BoolVar(&f.ignoreOutOfSyncTrunk, "ignore-out-of-sync-trunk", false, "Perform the submit operation even if the trunk branch is out of sync with its upstream branch.")
	cmd.Flags().BoolVar(&f.cli, "cli", false, "Edit PR metadata via the CLI instead of on web.")
	cmd.Flags().BoolVar(&f.noLabels, "no-labels", false, "Don't apply default labels from config.")
	cmd.Flags().BoolVar(&f.noAssignees, "no-assignees", false, "Don't apply default assignees from config.")
	cmd.Flags().BoolVar(&f.jsonOutput, "json", false, "Output the plan and per-branch results as JSON.")
}

func executeSubmit(cmd *cobra.Command, f *submitFlags) error {
	return common.Run(cmd, func(ctx *app.Context) error {
		// Get config values
		cfg, _ := config.LoadConfig(ctx.RepoRoot)
		submitFooter := cfg.SubmitFooter()

		// Run submit action
		stackRange := engine.StackRangeDownstack(true)
		if f.stack {
			stackRange = engine.StackRangeFull()
		}
		// Get config labels/assignees, respecting --no-labels and --no-assignees flags
		var configLabels []string
		var configAssignees []string
		if !f.noLabels {
			configLabels = cfg.SubmitLabels()
		}
		if !f.noAssignees {
			configAssignees = cfg.SubmitAssignees()
		}

		opts := submit.Options{
			Branch:               f.branch,
			StackRange:           stackRange,
			Force:                f.force,
			DryRun:               f.dryRun,
			Confirm:              f.confirm,
			UpdateOnly:           f.updateOnly,
			Always:               f.always,
			Restack:              f.restack,
			Draft:                f.draft,
			Publish:              f.publish,
			Edit:                 f.edit,
			EditTitle:            f.editTitle,
			EditDescription:      f.editDescription,
			NoEdit:               f.noEdit,
			NoEditTitle:          f.noEditTitle,
			NoEditDescription:    f.noEditDescription,
			Reviewers:            f.reviewers,
			TeamReviewers:        f.teamReviewers,
			MergeWhenReady:       f.mergeWhenReady,
			RerequestReview:      f.rerequestReview,
			View:                 f.view,
			Web:                  f.web,
			Comment:              f.comment,
			TargetTrunk:          f.targetTrunk,
			IgnoreOutOfSyncTrunk: f.ignoreOutOfSyncTrunk,
			SubmitFooter:         submitFooter,
			NoLabels:             f.noLabels,
			NoAssignees:          f.noAssignees,
			// Config-driven options
			ConfigDraft:     cfg.SubmitDraft(),
			ConfigWeb:       cfg.SubmitWeb(),
			ConfigLabels:    configLabels,
			ConfigReviewers: cfg.SubmitReviewers(),
			ConfigAssignees: configAssignees,
		}

		if f.jsonOutput {
			cmd.SilenceErrors = true
			jsonHandler := submit.NewJSONHandler()
			err := submit.Action(ctx, opts, jsonHandler)
			if err != nil {
				jsonHandler.SetError(err)
			}
			data, marshalErr := json.MarshalIndent(jsonHandler.Result, "", "  ")
			if marshalErr != nil {
				return fmt.Errorf("failed to marshal JSON: %w", marshalErr)
			}
			ctx.Output.Info("%s", string(data))
			return err
		}

		// Action is the single source of truth for what to submit. The runner
		// starts lazily (when the submission phase begins), so calling Action
		// unconditionally no longer flashes the TUI when there's nothing to do.
		runner, handler := NewSubmitUI(ctx.Output, ctx.Logger)
		defer runner.Cleanup()
		return submit.Action(ctx, opts, handler)
	})
}

// NewSubmitCmd creates the submit command
func NewSubmitCmd() *cobra.Command {
	f := &submitFlags{}

	cmd := &cobra.Command{
		Use:   "submit",
		Short: "Idempotently force push all branches in the current stack and create/update pull requests",
		Long: `Idempotently force push all branches in the current stack from trunk to the current branch to GitHub,
creating or updating distinct pull requests for each. Validates that branches are properly restacked before submitting,
and fails if there are conflicts. Blocks force pushes to branches that overwrite branches that have changed since
you last submitted or got them. Opens an interactive prompt that allows you to input pull request metadata.`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return executeSubmit(cmd, f)
		},
	}

	addSubmitFlags(cmd, f)

	return cmd
}

// NewSsCmd creates the ss command, which is an alias for submit --stack
func NewSsCmd() *cobra.Command {
	f := &submitFlags{}

	cmd := &cobra.Command{
		Use:          "ss",
		Hidden:       true, // Hide from main help to avoid clutter, but keep as alias
		Short:        "Alias for submit --stack",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			f.stack = true
			return executeSubmit(cmd, f)
		},
	}

	addSubmitFlags(cmd, f)

	return cmd
}
