// Package stack provides CLI commands for operating on entire stacks.
package stack

import (
	"io"
	"os"
	"sync"

	"github.com/spf13/cobra"

	"stackit.dev/stackit/internal/actions/submit"
	"stackit.dev/stackit/internal/app"
	"stackit.dev/stackit/internal/cli/common"
	"stackit.dev/stackit/internal/config"
	_ "stackit.dev/stackit/internal/demo" // Register demo engine factory
	"stackit.dev/stackit/internal/output"
	"stackit.dev/stackit/internal/tui"
	submitComponent "stackit.dev/stackit/internal/tui/components/submit"
	"stackit.dev/stackit/internal/tui/components/tree"
	"stackit.dev/stackit/internal/tui/style"
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
}

func executeSubmit(cmd *cobra.Command, f *submitFlags) error {
	return common.Run(cmd, func(ctx *app.Context) error {
		// Get config values
		cfg, _ := config.LoadConfig(ctx.RepoRoot)
		submitFooter := cfg.SubmitFooter()

		// Run submit action
		stackRange := submit.StackRangeDownstack()
		if f.stack {
			stackRange = submit.StackRangeFull()
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
		}

		// Create the appropriate handler based on TTY availability
		handler := NewSubmitHandler(ctx.Output, ctx.Logger)
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

// NewSubmitHandler creates the appropriate handler based on TTY availability
func NewSubmitHandler(out output.Output, logger output.Logger) submit.Handler {
	if tui.IsTTY() {
		return NewInteractiveSubmitHandler(out, logger)
	}
	return NewSimpleSubmitHandler(out)
}

// SimpleSubmitHandler implements submit.Handler with line-by-line output
type SimpleSubmitHandler struct {
	splog   output.Output
	out     io.Writer
	items   map[string]*branchItem
	mu      sync.Mutex
	started bool
}

type branchItem struct {
	name   string
	action string
}

// NewSimpleSubmitHandler creates a new simple submit handler
func NewSimpleSubmitHandler(splog output.Output) *SimpleSubmitHandler {
	return &SimpleSubmitHandler{
		splog: splog,
		out:   os.Stdout,
		items: make(map[string]*branchItem),
	}
}

// OnEvent handles events from the submit action
func (h *SimpleSubmitHandler) OnEvent(e submit.Event) {
	h.mu.Lock()
	defer h.mu.Unlock()

	switch ev := e.(type) {
	case submit.StackDisplayEvent:
		h.splog.Info("Stack to submit:")
		for _, branch := range ev.Stack.Branches {
			marker := "  "
			if branch == ev.Stack.CurrentBranch {
				marker = "● "
			}
			scope := ev.ScopeMap[branch]
			if scope != "" {
				h.splog.Info("%s%s [%s]", marker, branch, scope)
			} else {
				h.splog.Info("%s%s", marker, branch)
			}
		}
		h.splog.Newline()

	case submit.RestackEvent:
		if ev.Started {
			h.splog.Info("Restacking branches before submitting...")
		}
		// No output for completion

	case submit.PreparingEvent:
		// Skip - we'll show progress during actual submission

	case submit.BranchPlanEvent:
		displayName := ev.BranchName
		if ev.IsCurrent {
			displayName = ev.BranchName + " (current)"
		}
		if ev.Skipped {
			h.splog.Info("  ▸ %s %s", style.ColorDim(displayName), style.ColorDim("— "+ev.SkipReason))
		} else {
			h.splog.Info("  ▸ %s → %s", displayName, ev.Action)
		}

	case submit.SubmissionStartEvent:
		h.started = true
		for _, branch := range ev.Branches {
			h.items[branch.Name] = &branchItem{
				name:   branch.Name,
				action: branch.Action,
			}
		}
		h.splog.Newline()
		h.splog.Info("Submitting...")

	case submit.BranchProgressEvent:
		item := h.items[ev.BranchName]
		if item == nil {
			return
		}

		switch ev.Status {
		case submit.StatusSubmitting:
			action := "Creating"
			if item.action == "update" {
				action = "Updating"
			}
			h.splog.Info("  ⋯ %s %s...", ev.BranchName, action)

		case submit.StatusSyncing:
			h.splog.Info("  ⋯ %s syncing...", ev.BranchName)

		case submit.StatusDone:
			actionDone := "created"
			if item.action == "update" {
				actionDone = "updated"
			}
			h.splog.Info("  ✓ %s %s → %s", ev.BranchName, actionDone, ev.URL)

		case submit.StatusError:
			h.splog.Info("  ✗ %s failed: %v", ev.BranchName, ev.Error)
		}

	case submit.CompletionEvent:
		if !ev.Success && ev.Message != "" {
			h.splog.Info("%s", ev.Message)
		}
	}
}

// Confirm prompts for confirmation - in non-TTY mode, uses default
func (h *SimpleSubmitHandler) Confirm(_ string, defaultYes bool) (bool, error) {
	// Non-interactive, use default
	return defaultYes, nil
}

// InteractiveSubmitHandler implements submit.Handler with bubbletea for animated progress
type InteractiveSubmitHandler struct {
	out           output.Output
	logger        output.Logger
	runner        *tui.Runner
	model         *submitComponent.Model
	inSubmitPhase bool
	stack         *tree.StackTree
	fixedMap      map[string]bool
}

// NewInteractiveSubmitHandler creates a new interactive submit handler
func NewInteractiveSubmitHandler(out output.Output, logger output.Logger) *InteractiveSubmitHandler {
	return &InteractiveSubmitHandler{out: out, logger: logger}
}

// findRootBranch finds the root branch of the stack (the one whose parent is trunk)
func (h *InteractiveSubmitHandler) findRootBranch() string {
	if h.stack == nil || len(h.stack.Branches) == 0 {
		return ""
	}

	// If we're on the trunk branch, show everything from trunk down
	if h.stack.CurrentBranch == h.stack.TrunkBranch {
		return h.stack.TrunkBranch
	}

	// The root is the branch whose parent is trunk
	for _, branch := range h.stack.Branches {
		parent := h.stack.ParentMap[branch]
		if parent == h.stack.TrunkBranch {
			return branch
		}
	}
	// Fallback to first branch
	return h.stack.Branches[0]
}

func (h *InteractiveSubmitHandler) ensureProgramStarted() {
	if h.runner != nil {
		return
	}

	// Initialize model if needed
	if h.model == nil {
		h.model = submitComponent.NewModel(nil)
	}

	h.runner = tui.NewRunner(h.model, h.out, h.logger)
	h.runner.Start()
}

// Cleanup ensures the terminal is restored to normal mode.
func (h *InteractiveSubmitHandler) Cleanup() {
	if h.runner != nil {
		h.runner.Cleanup()
	}
}

// OnEvent handles events from the submit action
func (h *InteractiveSubmitHandler) OnEvent(e submit.Event) {
	switch ev := e.(type) {
	case submit.StackDisplayEvent:
		h.stack = ev.Stack
		h.fixedMap = ev.FixedMap

		// Build a tree renderer from the stack with custom fixed logic
		renderer := ev.Stack.ToRendererWithFixed(func(branchName string) bool {
			// Trunk is always "fixed" (never needs restack)
			if branchName == h.stack.TrunkBranch {
				return true
			}
			return h.fixedMap[branchName]
		})

		// Set scopes and other annotations
		items := make([]submitComponent.Item, 0, len(ev.Stack.Branches))
		for _, branchName := range ev.Stack.Branches {
			// Skip trunk - we don't submit it
			if branchName == ev.Stack.TrunkBranch {
				continue
			}

			scope := ev.ScopeMap[branchName]
			renderer.SetAnnotation(branchName, tree.BranchAnnotation{
				Scope:         scope,
				ExplicitScope: scope,
			})

			items = append(items, submitComponent.Item{
				BranchName: branchName,
				Action:     "thinking...",
				Status:     submitComponent.StatusPending,
			})
		}

		// Create model with tree renderer and initial items
		h.model = submitComponent.NewModel(items)
		h.model.Renderer = renderer
		h.model.RootBranch = h.findRootBranch()

		h.ensureProgramStarted()

	case submit.RestackEvent:
		h.ensureProgramStarted()
		if ev.Started {
			h.runner.Send(submitComponent.GlobalMessageMsg("Restacking branches..."))
		} else if ev.Completed {
			h.runner.Send(submitComponent.GlobalMessageMsg(""))
		}

	case submit.PreparingEvent:
		h.ensureProgramStarted()
		h.runner.Send(submitComponent.GlobalMessageMsg("Preparing branches..."))

	case submit.BranchPlanEvent:
		h.ensureProgramStarted()
		h.runner.Send(submitComponent.PlanUpdateMsg{
			BranchName: ev.BranchName,
			Action:     ev.Action,
			IsCurrent:  ev.IsCurrent,
			Skip:       ev.Skipped,
			SkipReason: ev.SkipReason,
		})

	case submit.SubmissionStartEvent:
		h.inSubmitPhase = true
		h.ensureProgramStarted()

		// Update items in the model
		for _, branch := range ev.Branches {
			item := submitComponent.Item{
				BranchName: branch.Name,
				Action:     branch.Action,
				PRNumber:   branch.PRNumber,
				Status:     "pending",
			}
			found := false
			for i, existing := range h.model.Items {
				if existing.BranchName == branch.Name {
					h.model.Items[i] = item
					found = true
					break
				}
			}
			if !found {
				h.model.Items = append(h.model.Items, item)
			}
		}

		h.runner.Send(submitComponent.GlobalMessageMsg("Submitting..."))

	case submit.BranchProgressEvent:
		if !h.inSubmitPhase || h.runner == nil {
			return
		}

		status := string(ev.Status)
		h.runner.Send(submitComponent.ProgressUpdateMsg{
			BranchName: ev.BranchName,
			Status:     status,
			URL:        ev.URL,
			Err:        ev.Error,
		})

	case submit.CompletionEvent:
		if h.runner == nil {
			return
		}

		if ev.Message != "" && ev.Message != "Submit complete" {
			h.runner.Send(submitComponent.GlobalMessageMsg(ev.Message))
		} else {
			h.runner.Send(submitComponent.GlobalMessageMsg(""))
		}
		h.runner.Send(submitComponent.ProgressCompleteMsg{})
		h.complete()
	}
}

// Confirm prompts for user confirmation
func (h *InteractiveSubmitHandler) Confirm(message string, defaultYes bool) (bool, error) {
	// Pause TUI for prompt
	if h.runner != nil {
		h.runner.Pause()
	}

	confirmed, err := tui.PromptConfirm(message, defaultYes)

	// Resume TUI
	if h.runner != nil {
		h.runner.Resume()
	}

	return confirmed, err
}

// complete finalizes the display
func (h *InteractiveSubmitHandler) complete() {
	if h.runner == nil {
		return
	}

	h.runner.Wait()
	h.runner.Cleanup()
}
