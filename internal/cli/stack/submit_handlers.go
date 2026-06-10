package stack

import (
	"fmt"

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
		// Start lazily when the submission phase begins rather than here: the
		// stack and plan print as plain lines, so a submit that turns out to
		// have nothing to do never flashes the bubbletea startup/teardown
		// sequence. See InteractiveSubmitHandler.OnEvent.
		return runner, NewInteractiveSubmitHandler(runner, model, out)
	}
	return nil, NewSimpleSubmitHandler(out)
}

// planPrinter prints the stack and per-branch plan as a single merged list,
// shared by both submit handlers.
type planPrinter struct {
	out         output.Output
	scopes      map[string]string
	worktrees   map[string]string
	headerShown bool
}

// SetStack stashes the stack annotations so plan lines can include them.
func (p *planPrinter) SetStack(stack submit.StackSnapshot) {
	p.scopes = stack.ScopeMap
	p.worktrees = stack.WorktreeMap
}

// PrintLine prints one branch of the plan, with the stack header before the
// first line.
func (p *planPrinter) PrintLine(ev submit.BranchPlanEvent) {
	if !p.headerShown {
		p.headerShown = true
		p.out.Info("Stack to submit:")
	}

	marker := "  "
	if ev.IsCurrent {
		marker = "● "
	}
	name := submitComponent.DisplayBranchName(ev.BranchName)
	if scope := p.scopes[ev.BranchName]; scope != "" {
		name += " [" + scope + "]"
	}
	if p.worktrees[ev.BranchName] != "" {
		name += " 📂 worktree"
	}

	if ev.Skipped {
		p.out.Info("%s%s %s", marker, style.ColorDim(name), style.ColorDim("— "+ev.SkipReason))
		return
	}

	action := ev.Action
	if ev.PRNumber != nil {
		action = fmt.Sprintf("%s #%d", action, *ev.PRNumber)
	}
	line := fmt.Sprintf("%s%s → %s", marker, name, action)
	if ev.Empty {
		line += " " + style.ColorDim("(empty)")
	}
	p.out.Info("%s", line)
}

// SimpleSubmitHandler implements submit.Handler with line-by-line output
type SimpleSubmitHandler struct {
	common.BaseHandler
	plan  planPrinter
	items map[string]*branchItem
	order []string
}

type branchItem struct {
	name         string
	action       string
	prNumber     *int
	url          string
	status       string
	err          error
	reportedDone bool
}

// NewSimpleSubmitHandler creates a new simple submit handler
func NewSimpleSubmitHandler(out output.Output) *SimpleSubmitHandler {
	return &SimpleSubmitHandler{
		BaseHandler: common.NewBaseHandler(out),
		plan:        planPrinter{out: out},
		items:       make(map[string]*branchItem),
	}
}

// OnEvent handles events from the submit action
func (h *SimpleSubmitHandler) OnEvent(e submit.Event) {
	h.Lock()
	defer h.Unlock()

	switch ev := e.(type) {
	case submit.StackDisplayEvent:
		// The stack and plan print as one merged list once plan events arrive.
		h.plan.SetStack(ev.Stack)

	case submit.RestackEvent:
		if ev.Started {
			h.Output.Info("Restacking branches before submitting...")
		}
		// No output for completion

	case submit.PreparingEvent:
		// Skip - we'll show progress during actual submission

	case submit.BranchPlanEvent:
		h.plan.PrintLine(ev)

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
			// Quiet: line-by-line output has no animation, so the transitional
			// state adds noise without information; only terminal states print.

		case submit.StatusSyncing:
			// Quiet: the metadata footer sync carries no new information; the
			// branch result was already reported when it first reached done.

		case submit.StatusDone:
			// The footer sync re-emits done; only report a branch once.
			if item.reportedDone {
				return
			}
			item.reportedDone = true
			actionDone := "created"
			if item.action == "update" {
				actionDone = "updated"
			}
			detail := actionDone
			if ref := submitComponent.PRRef(item.toSubmitItem()); ref != "" {
				detail = ref + " " + actionDone
			}
			h.Output.Info("  ✓ %s %s", submitComponent.DisplayBranchName(ev.BranchName), detail)

		case submit.StatusError:
			h.Output.Info("  ✗ %s failed: %v", submitComponent.DisplayBranchName(ev.BranchName), ev.Error)
		}

	case submit.BranchWarningEvent:
		h.Output.Warn("%s: %s", submitComponent.DisplayBranchName(ev.BranchName), ev.Warning)

	case submit.CompletionEvent:
		switch ev.Outcome {
		case submit.OutcomeComplete:
			if summary := submitComponent.FormatURLSummary(h.submitItems()); summary != "" {
				h.Output.Newline()
				h.Output.Info("%s", summary)
			}
		case submit.OutcomeFailed:
			// Quiet: the returned error is reported by the CLI; printing the
			// generic message here would double-report the failure.
		default:
			// Dry run, all up to date, canceled, nothing to submit: print the
			// outcome so the run doesn't end silently.
			if ev.Message != "" {
				h.Output.Info("%s", ev.Message)
			}
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
	plan          planPrinter
	inSubmitPhase bool
}

// NewInteractiveSubmitHandler creates a new interactive submit handler
func NewInteractiveSubmitHandler(runner *tui.Runner, model *submitComponent.Model, out output.Output) *InteractiveSubmitHandler {
	return &InteractiveSubmitHandler{runner: runner, model: model, out: out, plan: planPrinter{out: out}}
}

// OnEvent handles events from the submit action
func (h *InteractiveSubmitHandler) OnEvent(e submit.Event) {
	switch ev := e.(type) {
	case submit.StackDisplayEvent:
		// The stack and plan print as plain lines (see planPrinter); the TUI
		// only starts once there are branches to submit, so runs with no work
		// to do never flash the bubbletea startup/teardown sequence.
		h.plan.SetStack(ev.Stack)

	case submit.RestackEvent:
		if ev.Started {
			h.out.Info("Restacking branches before submitting...")
		}
		// No output for completion

	case submit.PreparingEvent:
		// Quiet - the plan lines follow immediately

	case submit.BranchPlanEvent:
		h.plan.PrintLine(ev)

	case submit.SubmissionStartEvent:
		h.inSubmitPhase = true

		items := make([]submitComponent.Item, len(ev.Branches))
		for i, branch := range ev.Branches {
			items[i] = submitComponent.Item{
				BranchName: branch.Name,
				Action:     branch.Action,
				PRNumber:   branch.PRNumber,
				Status:     submitComponent.StatusPending,
			}
		}
		h.model.Items = items

		// Start the TUI now that there's real submission work to animate.
		// Idempotent, so later events that arrive after this are safe.
		h.runner.Start()

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

	case submit.BranchWarningEvent:
		// The runner quiets console output while the TUI runs, so warnings must
		// flow through the model to be rendered and persisted.
		if h.runner.IsRunning() {
			h.runner.Send(submitComponent.WarningMsg{
				BranchName: ev.BranchName,
				Warning:    ev.Warning,
			})
			return
		}
		h.out.Warn("%s: %s", submitComponent.DisplayBranchName(ev.BranchName), ev.Warning)

	case submit.CompletionEvent:
		// If the submission phase never started (nothing to submit, dry run,
		// canceled), the TUI isn't running — print the outcome plainly.
		if !h.runner.IsRunning() {
			if ev.Outcome != submit.OutcomeFailed && ev.Message != "" {
				h.out.Info("%s", ev.Message)
			}
			return
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
