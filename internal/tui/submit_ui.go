package tui

import (
	"fmt"
	"os"
	"sync"

	tea "github.com/charmbracelet/bubbletea"

	"stackit.dev/stackit/internal/output"
	"stackit.dev/stackit/internal/splog"
	"stackit.dev/stackit/internal/tui/components/submit"
	"stackit.dev/stackit/internal/tui/components/tree"
	"stackit.dev/stackit/internal/tui/style"
)

// SubmitUI defines the interface for the full submit workflow display
type SubmitUI interface {
	// ShowStack displays the stack to be submitted
	ShowStack(renderer *tree.StackTreeRenderer, rootBranch string)

	// ShowRestack shows restack progress
	ShowRestackStart()
	ShowRestackComplete()

	// ShowPreparing shows the preparation phase
	ShowPreparing()

	// ShowBranchPlan shows what will happen to a branch
	ShowBranchPlan(branchName string, action string, isCurrent bool, skip bool, skipReason string)

	// ShowNoChanges indicates all PRs are up to date
	ShowNoChanges()

	// ShowDryRunComplete indicates dry run is complete
	ShowDryRunComplete()

	// StartSubmitting begins the submission phase
	StartSubmitting(items []submit.Item)

	// UpdateSubmitItem updates status during submission
	UpdateSubmitItem(branchName string, status string, url string, err error)

	// Pause suspends the UI (for interactive prompts)
	Pause()

	// Resume resumes the UI
	Resume()

	// Complete finalizes and shows summary
	Complete()
}

// NewSubmitUI creates the appropriate UI based on TTY availability
func NewSubmitUI(splog output.Output) SubmitUI {
	if IsTTY() {
		return NewTTYSubmitUI(splog)
	}
	return NewSimpleSubmitUI(splog)
}

// SimpleSubmitUI implements SubmitUI with line-by-line output
type SimpleSubmitUI struct {
	splog     output.Output
	items     []submit.Item
	completed int
	failed    int
	mu        sync.Mutex
}

// NewSimpleSubmitUI creates a new simple submit UI
func NewSimpleSubmitUI(splog output.Output) *SimpleSubmitUI {
	return &SimpleSubmitUI{splog: splog}
}

// ShowStack displays the branch stack being submitted
func (u *SimpleSubmitUI) ShowStack(renderer *tree.StackTreeRenderer, rootBranch string) {
	u.splog.Info("Stack to submit:")
	lines := renderer.RenderStack(rootBranch, tree.RenderOptions{
		HideStats: true,
	})
	for _, line := range lines {
		u.splog.Info("%s", line)
	}
	u.splog.Newline()
}

// ShowRestackStart indicates the start of the restack process
func (u *SimpleSubmitUI) ShowRestackStart() {
	u.splog.Info("Restacking branches before submitting...")
}

// ShowRestackComplete indicates the completion of the restack process
func (u *SimpleSubmitUI) ShowRestackComplete() {
	// Nothing needed for simple UI
}

// ShowPreparing indicates the preparation phase
func (u *SimpleSubmitUI) ShowPreparing() {
	// Skip - we'll show progress during actual submission
}

// ShowBranchPlan indicates the action planned for a branch
func (u *SimpleSubmitUI) ShowBranchPlan(branchName string, _ string, isCurrent bool, skip bool, skipReason string) {
	// Only show if skipping (important info), otherwise we'll show during submission
	if skip {
		displayName := branchName
		if isCurrent {
			displayName = branchName + " (current)"
		}
		u.splog.Info("  ▸ %s %s", style.ColorDim(displayName), style.ColorDim("— "+skipReason))
	}
}

// ShowNoChanges indicates no changes were detected
func (u *SimpleSubmitUI) ShowNoChanges() {
	u.splog.Info("All PRs up to date.")
}

// ShowDryRunComplete indicates completion of a dry run
func (u *SimpleSubmitUI) ShowDryRunComplete() {
	u.splog.Info("Dry run complete.")
}

// StartSubmitting begins the actual submission phase
func (u *SimpleSubmitUI) StartSubmitting(items []submit.Item) {
	u.mu.Lock()
	defer u.mu.Unlock()

	u.items = items
	u.completed = 0
	u.failed = 0
	u.splog.Newline()
	u.splog.Info("Submitting...")
}

// UpdateSubmitItem updates the status of a specific branch submission
func (u *SimpleSubmitUI) UpdateSubmitItem(branchName string, status string, url string, err error) {
	u.mu.Lock()
	defer u.mu.Unlock()

	var item *submit.Item
	var itemIdx int
	for i := range u.items {
		if u.items[i].BranchName == branchName {
			item = &u.items[i]
			itemIdx = i
			break
		}
	}

	if item == nil {
		return
	}

	const (
		statusSubmitting = "submitting"
		statusDone       = "done"
		statusError      = "error"
		actionUpdate     = "update"
	)

	switch status {
	case statusSubmitting:
		const (
			labelCreating = "Creating"
			labelUpdating = "Updating"
		)
		action := labelCreating
		if item.Action == actionUpdate {
			action = labelUpdating
		}
		u.splog.Info("  ⋯ %s %s...", item.BranchName, action)

	case statusDone:
		u.completed++
		const (
			actionCreated = "created"
			actionUpdated = "updated"
		)
		actionDone := actionCreated
		if item.Action == actionUpdate {
			actionDone = actionUpdated
		}
		u.splog.Info("  ✓ %s %s → %s", item.BranchName, actionDone, url)

	case statusError:
		u.failed++
		u.splog.Info("  ✗ %s failed: %v", item.BranchName, err)
	}

	u.items[itemIdx].Status = status
	u.items[itemIdx].URL = url
	u.items[itemIdx].Error = err
}

// Pause is a no-op for simple UI
func (u *SimpleSubmitUI) Pause() {}

// Resume is a no-op for simple UI
func (u *SimpleSubmitUI) Resume() {}

// Complete finalizes the display and shows a summary
func (u *SimpleSubmitUI) Complete() {
	u.mu.Lock()
	defer u.mu.Unlock()

	if u.failed > 0 {
		u.splog.Newline()
		u.splog.Info("Completed: %d, Failed: %d", u.completed, u.failed)
	}
}

// TTYSubmitUI implements SubmitUI with bubbletea for animated progress
type TTYSubmitUI struct {
	splog         output.Output
	program       *tea.Program
	model         *submit.Model
	inSubmitPhase bool
	mu            sync.Mutex
}

// NewTTYSubmitUI creates a new TTY submit UI
func NewTTYSubmitUI(splog output.Output) *TTYSubmitUI {
	return &TTYSubmitUI{splog: splog}
}

func (u *TTYSubmitUI) ensureProgramStarted() {
	u.mu.Lock()
	defer u.mu.Unlock()

	if u.program != nil {
		return
	}

	// Quiet the splog so it doesn't interfere with the TUI
	u.splog.SetQuiet(true)

	u.program = tea.NewProgram(u.model, tea.WithInput(os.Stdin), tea.WithOutput(os.Stdout))

	// Run program in background
	splog.SafeGo(func() {
		if _, err := u.program.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "Error running submit TUI: %v\n", err)
		}
	})
}

// ShowStack displays the branch stack being submitted
func (u *TTYSubmitUI) ShowStack(renderer *tree.StackTreeRenderer, rootBranch string) {
	u.model = submit.NewModel(nil)
	u.model.Renderer = renderer
	u.model.RootBranch = rootBranch

	u.ensureProgramStarted()
}

// ShowRestackStart indicates the start of the restack process
func (u *TTYSubmitUI) ShowRestackStart() {
	u.ensureProgramStarted()
	u.program.Send(submit.GlobalMessageMsg("Restacking branches..."))
}

// ShowRestackComplete indicates the completion of the restack process
func (u *TTYSubmitUI) ShowRestackComplete() {
	if u.program != nil {
		u.program.Send(submit.GlobalMessageMsg(""))
	}
}

// ShowPreparing indicates the preparation phase
func (u *TTYSubmitUI) ShowPreparing() {
	u.ensureProgramStarted()
	u.program.Send(submit.GlobalMessageMsg("Preparing branches..."))
}

// ShowBranchPlan indicates the action planned for a branch
func (u *TTYSubmitUI) ShowBranchPlan(branchName string, action string, isCurrent bool, skip bool, skipReason string) {
	u.ensureProgramStarted()
	u.program.Send(submit.PlanUpdateMsg{
		BranchName: branchName,
		Action:     action,
		IsCurrent:  isCurrent,
		Skip:       skip,
		SkipReason: skipReason,
	})
}

// ShowNoChanges indicates no changes were detected
func (u *TTYSubmitUI) ShowNoChanges() {
	u.ensureProgramStarted()
	u.program.Send(submit.GlobalMessageMsg("All PRs up to date."))
	u.program.Send(submit.ProgressCompleteMsg{})
}

// ShowDryRunComplete indicates completion of a dry run
func (u *TTYSubmitUI) ShowDryRunComplete() {
	u.ensureProgramStarted()
	u.program.Send(submit.GlobalMessageMsg("Dry run complete."))
	u.program.Send(submit.ProgressCompleteMsg{})
}

// StartSubmitting begins the actual submission phase
func (u *TTYSubmitUI) StartSubmitting(items []submit.Item) {
	u.inSubmitPhase = true

	// Update items in the model
	for _, newItem := range items {
		found := false
		for i, item := range u.model.Items {
			if item.BranchName == newItem.BranchName {
				u.model.Items[i].Status = newItem.Status
				u.model.Items[i].Action = newItem.Action
				u.model.Items[i].PRNumber = newItem.PRNumber
				found = true
				break
			}
		}
		if !found {
			u.model.Items = append(u.model.Items, newItem)
		}
	}

	u.ensureProgramStarted()
	u.program.Send(submit.GlobalMessageMsg("Submitting..."))
}

// UpdateSubmitItem updates the status of a specific branch submission
func (u *TTYSubmitUI) UpdateSubmitItem(branchName string, status string, url string, err error) {
	if !u.inSubmitPhase || u.program == nil {
		return
	}
	u.program.Send(submit.ProgressUpdateMsg{
		BranchName: branchName,
		Status:     status,
		URL:        url,
		Err:        err,
	})
}

// Pause suspends the UI for interactive prompts
func (u *TTYSubmitUI) Pause() {
	if u.program != nil {
		_ = u.program.ReleaseTerminal()
		u.splog.SetQuiet(false)
	}
}

// Resume resumes the UI after interactive prompts
func (u *TTYSubmitUI) Resume() {
	if u.program != nil {
		u.splog.SetQuiet(true)
		_ = u.program.RestoreTerminal()
	}
}

// Complete finalizes the display and shows a summary
func (u *TTYSubmitUI) Complete() {
	if u.program == nil {
		return
	}

	// If we're already done or didn't start the submit phase, just quit.
	// Otherwise, send completion messages.
	if u.inSubmitPhase {
		u.program.Send(submit.GlobalMessageMsg(""))
		u.program.Send(submit.ProgressCompleteMsg{})
	} else {
		u.program.Quit()
	}

	u.program.Wait()
	u.program = nil
	u.splog.SetQuiet(false)
}
