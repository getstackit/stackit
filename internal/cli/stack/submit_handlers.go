package stack

import (
	"github.com/getstackit/stackit/internal/actions/submit"
	"github.com/getstackit/stackit/internal/cli/common"
	"github.com/getstackit/stackit/internal/output"
	"github.com/getstackit/stackit/internal/tui"
	submitComponent "github.com/getstackit/stackit/internal/tui/components/submit"
	"github.com/getstackit/stackit/internal/tui/style"
)

// NewSubmitUI creates a runner and handler pair for submit operations.
// The runner manages terminal state; the handler processes events.
// Caller must defer runner.Cleanup() to restore terminal on exit.
func NewSubmitUI(out output.Output, logger output.Logger) (*tui.Runner, submit.Handler) {
	if tui.IsTTY() {
		model := submitComponent.NewModel(nil)
		runner := tui.NewRunner(model, out, logger)
		// Start lazily on the first stack-display event rather than here, so a
		// submit that turns out to have nothing to do never flashes the bubbletea
		// startup/teardown sequence. See InteractiveSubmitHandler.OnEvent.
		return runner, NewInteractiveSubmitHandler(runner, model, out)
	}
	return nil, NewSimpleSubmitHandler(out)
}

// SimpleSubmitHandler implements submit.Handler with line-by-line output
type SimpleSubmitHandler struct {
	common.BaseHandler
	items     map[string]*branchItem
	order     []string
	displayed bool
}

type branchItem struct {
	name     string
	action   string
	prNumber *int
	url      string
	status   string
	err      error
}

// NewSimpleSubmitHandler creates a new simple submit handler
func NewSimpleSubmitHandler(out output.Output) *SimpleSubmitHandler {
	return &SimpleSubmitHandler{
		BaseHandler: common.NewBaseHandler(out),
		items:       make(map[string]*branchItem),
	}
}

// OnEvent handles events from the submit action
func (h *SimpleSubmitHandler) OnEvent(e submit.Event) {
	h.Lock()
	defer h.Unlock()

	switch ev := e.(type) {
	case submit.StackDisplayEvent:
		h.displayed = true
		h.Output.Info("Stack to submit:")
		for _, branch := range ev.Stack.Branches {
			marker := "  "
			if branch == ev.Stack.CurrentBranch {
				marker = "● "
			}
			scope := ev.Stack.ScopeMap[branch]
			worktree := ev.Stack.WorktreeMap[branch]

			var line string
			if scope != "" {
				line = marker + submitComponent.DisplayBranchName(branch) + " [" + scope + "]"
			} else {
				line = marker + submitComponent.DisplayBranchName(branch)
			}
			if worktree != "" {
				line += " 📂 worktree"
			}
			h.Output.Info("%s", line)
		}
		h.Output.Newline()

	case submit.RestackEvent:
		if ev.Started {
			h.Output.Info("Restacking branches before submitting...")
		}
		// No output for completion

	case submit.PreparingEvent:
		// Skip - we'll show progress during actual submission

	case submit.BranchPlanEvent:
		displayName := submitComponent.DisplayBranchName(ev.BranchName)
		if ev.IsCurrent {
			displayName += " (current)"
		}
		if ev.Skipped {
			h.Output.Info("  ▸ %s %s", style.ColorDim(displayName), style.ColorDim("— "+ev.SkipReason))
		} else {
			h.Output.Info("  ▸ %s → %s", displayName, ev.Action)
		}

	case submit.SubmissionStartEvent:
		h.order = h.order[:0]
		for _, branch := range ev.Branches {
			h.items[branch.Name] = &branchItem{
				name:     branch.Name,
				action:   branch.Action,
				prNumber: branch.PRNumber,
				status:   string(submit.StatusPending),
			}
			h.order = append(h.order, branch.Name)
		}
		h.Output.Newline()
		h.Output.Info("Submitting %d %s", len(ev.Branches), pluralizeBranches(len(ev.Branches)))

	case submit.BranchProgressEvent:
		item := h.items[ev.BranchName]
		if item == nil {
			return
		}
		item.status = string(ev.Status)
		if ev.URL != "" {
			item.url = ev.URL
		}
		if ev.Error != nil {
			item.err = ev.Error
		}

		switch ev.Status {
		case submit.StatusSubmitting:
			action := "Creating"
			if item.action == "update" {
				action = "Updating"
			}
			h.Output.Info("  ⋯ %s %s...", submitComponent.DisplayBranchName(ev.BranchName), action)

		case submit.StatusSyncing:
			h.Output.Info("  ⋯ %s syncing...", submitComponent.DisplayBranchName(ev.BranchName))

		case submit.StatusDone:
			actionDone := "created"
			if item.action == "update" {
				actionDone = "updated"
			}
			ref := submitComponent.PRRef(item.toSubmitItem())
			if ref != "" {
				h.Output.Info("  ✓ %s %s", submitComponent.DisplayBranchName(ev.BranchName), ref)
			} else {
				h.Output.Info("  ✓ %s %s", submitComponent.DisplayBranchName(ev.BranchName), actionDone)
			}

		case submit.StatusError:
			h.Output.Info("  ✗ %s failed: %v", submitComponent.DisplayBranchName(ev.BranchName), ev.Error)
		}

	case submit.CompletionEvent:
		switch {
		case !ev.Success && ev.Message != "":
			h.Output.Info("%s", ev.Message)
		case ev.Success && ev.Message == "Submit complete":
			if summary := submitComponent.FormatURLSummary(h.submitItems()); summary != "" {
				h.Output.Newline()
				h.Output.Info("%s", summary)
			}
		case ev.Success && !h.displayed && ev.Message != "":
			// No stack was ever displayed (empty scope / nothing to submit).
			// Surface the outcome that the CLI used to print before delegating
			// work-detection to Action. Cases that show a stack first (dry run,
			// all up to date) already convey the outcome via the plan lines.
			h.Output.Info("%s", ev.Message)
		}
	}
}

func (h *SimpleSubmitHandler) submitItems() []submitComponent.Item {
	items := make([]submitComponent.Item, 0, len(h.order))
	for _, name := range h.order {
		item := h.items[name]
		if item == nil {
			continue
		}
		items = append(items, item.toSubmitItem())
	}
	return items
}

func (i *branchItem) toSubmitItem() submitComponent.Item {
	if i == nil {
		return submitComponent.Item{}
	}
	return submitComponent.Item{
		BranchName: i.name,
		Action:     i.action,
		PRNumber:   i.prNumber,
		Status:     i.status,
		URL:        i.url,
		Error:      i.err,
	}
}

func pluralizeBranches(count int) string {
	if count == 1 {
		return "branch"
	}
	return "branches"
}

// Confirm prompts for confirmation - in non-TTY mode, uses default
func (h *SimpleSubmitHandler) Confirm(_ string, defaultYes bool) (bool, error) {
	// Non-interactive, use default
	return defaultYes, nil
}

// InteractiveSubmitHandler implements submit.Handler with bubbletea for animated progress
type InteractiveSubmitHandler struct {
	runner        *tui.Runner
	model         *submitComponent.Model
	out           output.Output
	inSubmitPhase bool
}

// NewInteractiveSubmitHandler creates a new interactive submit handler
func NewInteractiveSubmitHandler(runner *tui.Runner, model *submitComponent.Model, out output.Output) *InteractiveSubmitHandler {
	return &InteractiveSubmitHandler{runner: runner, model: model, out: out}
}

// OnEvent handles events from the submit action
func (h *InteractiveSubmitHandler) OnEvent(e submit.Event) {
	switch ev := e.(type) {
	case submit.StackDisplayEvent:
		items := make([]submitComponent.Item, 0, len(ev.Stack.Branches))
		for _, branchName := range ev.Stack.Branches {
			// Skip trunk - we don't submit it
			if branchName == ev.Stack.TrunkBranch {
				continue
			}

			items = append(items, submitComponent.Item{
				BranchName: branchName,
				Action:     "thinking...",
				Status:     submitComponent.StatusPending,
			})
		}

		h.model.Items = items

		// Start the TUI now that there's a populated stack to show. Idempotent,
		// so later events that arrive after this are safe.
		h.runner.Start()

	case submit.RestackEvent:
		if ev.Started {
			h.runner.Send(submitComponent.GlobalMessageMsg("Restacking branches..."))
		} else if ev.Completed {
			h.runner.Send(submitComponent.GlobalMessageMsg(""))
		}

	case submit.PreparingEvent:
		h.runner.Send(submitComponent.GlobalMessageMsg("Preparing branches..."))

	case submit.BranchPlanEvent:
		h.runner.Send(submitComponent.PlanUpdateMsg{
			BranchName: ev.BranchName,
			Action:     ev.Action,
			IsCurrent:  ev.IsCurrent,
			Skip:       ev.Skipped,
			SkipReason: ev.SkipReason,
		})

	case submit.SubmissionStartEvent:
		h.inSubmitPhase = true

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
		if !h.inSubmitPhase {
			return
		}

		h.runner.Send(submitComponent.ProgressUpdateMsg{
			BranchName: ev.BranchName,
			Status:     string(ev.Status),
			URL:        ev.URL,
			Err:        ev.Error,
		})

	case submit.CompletionEvent:
		// If no stack was ever displayed (e.g. nothing to submit), the TUI was
		// never started — print the outcome plainly instead of routing it
		// through a runner that isn't running.
		if !h.runner.IsRunning() {
			if ev.Message != "" {
				h.out.Info("%s", ev.Message)
			}
			return
		}
		if ev.Message != "" && ev.Message != "Submit complete" {
			h.runner.Send(submitComponent.GlobalMessageMsg(ev.Message))
		} else {
			h.runner.Send(submitComponent.GlobalMessageMsg(""))
		}
		h.runner.Send(submitComponent.ProgressCompleteMsg{})
		h.runner.Wait()
	}
}

// Confirm prompts for user confirmation
func (h *InteractiveSubmitHandler) Confirm(message string, defaultYes bool) (bool, error) {
	h.runner.Pause()
	confirmed, err := tui.PromptConfirm(message, defaultYes)
	h.runner.Resume()
	return confirmed, err
}

// IsInteractive returns true - interactive handler supports prompts.
func (h *InteractiveSubmitHandler) IsInteractive() bool {
	return true
}
