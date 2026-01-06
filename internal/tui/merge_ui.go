package tui

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"stackit.dev/stackit/internal/github"
	"stackit.dev/stackit/internal/tui/style"
)

// MergeGroup represents a group of steps that should be displayed as a single line
type MergeGroup struct {
	Label       string
	StepIndices []int
}

// MergeStepItem represents a step in the merge process
type MergeStepItem struct {
	StepIndex   int
	Description string
	Status      string // "pending", "running", "done", "error", "waiting"
	Error       error
	WaitElapsed time.Duration
	WaitTimeout time.Duration
	Checks      []github.CheckDetail
}

// MergeTUIModel is the bubbletea model for merge progress
type MergeTUIModel struct {
	groups            []MergeGroup
	steps             []MergeStepItem
	currentIdx        int
	spinner           spinner.Model
	done              bool
	quitting          bool
	styles            mergeStyles
	updates           <-chan ProgressUpdate
	doneChan          chan<- bool
	estimatedDuration time.Duration
}

type mergeStyles struct {
	spinnerStyle lipgloss.Style
	doneStyle    lipgloss.Style
	errorStyle   lipgloss.Style
	waitStyle    lipgloss.Style
	dimStyle     lipgloss.Style
	timeStyle    lipgloss.Style
}

const (
	mergeStatusPending = "pending"
	mergeStatusRunning = "running"
	mergeStatusWaiting = "waiting"
	mergeStatusDone    = "done"
	mergeStatusError   = "error"
)

const (
	dotSymbol        = "●"
	statusCompleted  = "COMPLETED"
	statusInProgress = "IN_PROGRESS"
	statusQueued     = "QUEUED"
)

// StepUpdateMsg is sent when a step status changes
type StepUpdateMsg struct {
	StepIndex int
	Status    string
	Error     error
}

// StepWaitUpdateMsg is sent to update wait timer
type StepWaitUpdateMsg struct {
	StepIndex int
	Elapsed   time.Duration
	Timeout   time.Duration
	Checks    []github.CheckDetail
}

// EstimatedDurationMsg is sent when the total estimated duration is updated
type EstimatedDurationMsg time.Duration

// NewMergeTUIModel creates a new merge TUI model
func NewMergeTUIModel(groups []MergeGroup, stepDescriptions []string) MergeTUIModel {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))

	steps := make([]MergeStepItem, len(stepDescriptions))
	for i, desc := range stepDescriptions {
		steps[i] = MergeStepItem{
			StepIndex:   i,
			Description: desc,
			Status:      mergeStatusPending,
		}
	}

	// If no groups provided, create one group per step
	if len(groups) == 0 {
		groups = make([]MergeGroup, len(stepDescriptions))
		for i, desc := range stepDescriptions {
			groups[i] = MergeGroup{
				Label:       desc,
				StepIndices: []int{i},
			}
		}
	}

	return MergeTUIModel{
		groups:     groups,
		steps:      steps,
		currentIdx: 0,
		spinner:    s,
		styles: mergeStyles{
			spinnerStyle: lipgloss.NewStyle().Foreground(lipgloss.Color("205")),
			doneStyle:    lipgloss.NewStyle().Foreground(lipgloss.Color("42")),
			errorStyle:   lipgloss.NewStyle().Foreground(lipgloss.Color("196")),
			waitStyle:    lipgloss.NewStyle().Foreground(lipgloss.Color("214")),
			dimStyle:     style.SubtleStyle(),
			timeStyle:    style.DimStyle(),
		},
	}
}

// Init initializes the bubbletea model
func (m MergeTUIModel) Init() tea.Cmd {
	// Start spinner and update ticker
	return tea.Batch(m.spinner.Tick, m.checkForUpdates())
}

// checkForUpdates checks for updates from the channel
func (m MergeTUIModel) checkForUpdates() tea.Cmd {
	if m.updates == nil {
		return nil
	}

	return tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg {
		select {
		case update, ok := <-m.updates:
			if !ok {
				return tea.Quit()
			}

			var msg tea.Msg
			switch update.Type {
			case "started":
				msg = StepUpdateMsg{
					StepIndex: update.StepIndex,
					Status:    mergeStatusRunning,
				}
			case "completed":
				msg = StepUpdateMsg{
					StepIndex: update.StepIndex,
					Status:    mergeStatusDone,
				}
			case "failed":
				msg = StepUpdateMsg{
					StepIndex: update.StepIndex,
					Status:    mergeStatusError,
					Error:     update.Error,
				}
			case "waiting":
				msg = StepWaitUpdateMsg{
					StepIndex: update.StepIndex,
					Elapsed:   update.Elapsed,
					Timeout:   update.Timeout,
					Checks:    update.Checks,
				}
			case "estimate":
				msg = EstimatedDurationMsg(update.EstimatedDuration)
			}
			return msg
		default:
			// No update available, return nil to continue polling
			return nil
		}
	})
}

// Update handles message updates for the bubbletea model
func (m MergeTUIModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if msg, ok := msg.(tea.KeyMsg); ok {
		if msg.String() == KeyCtrlC || msg.String() == KeyQuit {
			m.quitting = true
			return m, tea.Quit
		}
	}

	switch msg := msg.(type) {
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		// Also check for updates
		return m, tea.Batch(cmd, m.checkForUpdates())

	case StepUpdateMsg:
		if msg.StepIndex < len(m.steps) {
			m.steps[msg.StepIndex].Status = msg.Status
			if msg.Error != nil {
				m.steps[msg.StepIndex].Error = msg.Error
			}
			// If step completed, move to next
			if msg.Status == mergeStatusDone && msg.StepIndex == m.currentIdx {
				m.currentIdx++
				if m.currentIdx == len(m.steps) {
					m.done = true
				}
			}
			// If step failed, mark as done
			if msg.Status == mergeStatusError {
				m.done = true
			}
		}
		// Continue checking for updates
		return m, m.checkForUpdates()

	case StepWaitUpdateMsg:
		if msg.StepIndex < len(m.steps) {
			m.steps[msg.StepIndex].WaitElapsed = msg.Elapsed
			m.steps[msg.StepIndex].WaitTimeout = msg.Timeout
			m.steps[msg.StepIndex].Checks = msg.Checks
			m.steps[msg.StepIndex].Status = mergeStatusWaiting
		}
		// Continue checking for updates
		return m, m.checkForUpdates()

	case EstimatedDurationMsg:
		m.estimatedDuration = time.Duration(msg)
		return m, m.checkForUpdates()

	case tea.QuitMsg:
		return m, tea.Quit
	}

	return m, nil
}

// View renders the TUI
func (m MergeTUIModel) View() string {
	if m.quitting {
		return ""
	}

	var b strings.Builder
	b.WriteString("\n")

	// Calculate overall progress
	completedGroups := 0
	totalGroups := len(m.groups)
	currentGroupIdx := -1
	for i, group := range m.groups {
		allDone := true
		for _, idx := range group.StepIndices {
			if m.steps[idx].Status != mergeStatusDone {
				allDone = false
				break
			}
		}
		if allDone {
			completedGroups++
		} else if currentGroupIdx == -1 {
			currentGroupIdx = i
		}
	}

	// Header with progress indicator
	header := lipgloss.NewStyle().Bold(true).Render("Merge Progress")
	progressIndicator := m.styles.dimStyle.Render(fmt.Sprintf("  Step %d of %d", completedGroups+1, totalGroups))
	if m.done {
		progressIndicator = m.styles.doneStyle.Render(fmt.Sprintf("  %d of %d complete", completedGroups, totalGroups))
	}
	b.WriteString(header + progressIndicator)
	b.WriteString("\n\n")

	// Categorize groups by status
	type groupInfo struct {
		index      int
		status     string
		activeStep *MergeStepItem
		failedStep *MergeStepItem
	}

	var completedGroupInfos []groupInfo
	var runningGroupInfo *groupInfo
	var pendingGroupInfos []groupInfo

	for i, group := range m.groups {
		var status string
		var activeStep *MergeStepItem
		var failedStep *MergeStepItem

		allDone := true
		allPending := true

		for _, idx := range group.StepIndices {
			step := &m.steps[idx]
			if step.Status == mergeStatusError {
				failedStep = step
				status = mergeStatusError
				break
			}
			if step.Status != mergeStatusDone {
				allDone = false
			}
			if step.Status != mergeStatusPending {
				allPending = false
			}
			if (step.Status == mergeStatusRunning || step.Status == mergeStatusWaiting) && activeStep == nil {
				activeStep = step
			}
		}

		if status != mergeStatusError {
			switch {
			case allDone:
				status = mergeStatusDone
			case allPending:
				status = mergeStatusPending
			default:
				status = mergeStatusRunning
			}
		}

		info := groupInfo{index: i, status: status, activeStep: activeStep, failedStep: failedStep}
		switch status {
		case mergeStatusDone:
			completedGroupInfos = append(completedGroupInfos, info)
		case mergeStatusRunning, mergeStatusError:
			runningGroupInfo = &info
		case mergeStatusPending:
			pendingGroupInfos = append(pendingGroupInfos, info)
		}
	}

	// Render with compact view: show last 2 completed, current, next 2 pending
	const maxCompletedToShow = 2
	const maxPendingToShow = 2

	// Show ellipsis if we're hiding completed groups
	if len(completedGroupInfos) > maxCompletedToShow {
		b.WriteString(m.styles.dimStyle.Render(fmt.Sprintf("  ... %d completed\n", len(completedGroupInfos)-maxCompletedToShow)))
	}

	// Show last N completed groups
	startIdx := 0
	if len(completedGroupInfos) > maxCompletedToShow {
		startIdx = len(completedGroupInfos) - maxCompletedToShow
	}
	for _, info := range completedGroupInfos[startIdx:] {
		group := m.groups[info.index]
		b.WriteString(fmt.Sprintf("  %s %s\n", m.styles.doneStyle.Render("✓"), m.styles.doneStyle.Render(group.Label)))
	}

	// Show current running/error group with full details
	if runningGroupInfo != nil {
		group := m.groups[runningGroupInfo.index]
		var line strings.Builder

		if runningGroupInfo.status == mergeStatusError {
			line.WriteString(fmt.Sprintf("  %s %s ", m.styles.errorStyle.Render("✗"), m.styles.errorStyle.Render(group.Label)))
			if runningGroupInfo.failedStep != nil && runningGroupInfo.failedStep.Error != nil {
				line.WriteString(m.styles.errorStyle.Render("→ " + runningGroupInfo.failedStep.Error.Error()))
			}
		} else {
			line.WriteString(fmt.Sprintf("  %s %s ", m.spinner.View(), lipgloss.NewStyle().Bold(true).Render(group.Label)))

			if runningGroupInfo.activeStep != nil {
				if runningGroupInfo.activeStep.Status == mergeStatusWaiting {
					elapsed := runningGroupInfo.activeStep.WaitElapsed.Round(time.Second)

					if len(runningGroupInfo.activeStep.Checks) > 0 {
						line.WriteString(m.renderCheckIndicators(runningGroupInfo.activeStep.Checks))
						line.WriteString(" ")
					}

					if m.estimatedDuration > 0 {
						line.WriteString(m.renderProgressBar(elapsed, m.estimatedDuration))
						line.WriteString(" ")
					}

					line.WriteString(m.styles.timeStyle.Render(fmt.Sprintf("%v elapsed", elapsed)))
					line.WriteString("\n")
					line.WriteString(m.renderDetailedChecks(runningGroupInfo.activeStep.Checks))
				} else {
					desc := runningGroupInfo.activeStep.Description
					switch {
					case strings.HasPrefix(desc, "Merge PR"):
						desc = "merging"
					case strings.HasPrefix(desc, "Delete local branch"):
						desc = "deleting"
					case strings.HasPrefix(desc, "Restack"):
						desc = "restacking"
					case strings.HasPrefix(desc, "Consolidate"):
						desc = "consolidating"
					}
					line.WriteString(m.styles.spinnerStyle.Render("[" + desc + "]"))
				}
			}
		}
		b.WriteString(line.String())
		b.WriteString("\n")
	}

	// Show next N pending groups
	pendingToShow := pendingGroupInfos
	if len(pendingToShow) > maxPendingToShow {
		pendingToShow = pendingToShow[:maxPendingToShow]
	}
	for _, info := range pendingToShow {
		group := m.groups[info.index]
		b.WriteString(fmt.Sprintf("  %s %s\n", m.styles.dimStyle.Render("○"), m.styles.dimStyle.Render(group.Label)))
	}

	// Show ellipsis if we're hiding pending groups
	if len(pendingGroupInfos) > maxPendingToShow {
		b.WriteString(m.styles.dimStyle.Render(fmt.Sprintf("  ... %d more\n", len(pendingGroupInfos)-maxPendingToShow)))
	}

	if m.done {
		// ...
		completedGroups := 0
		failedGroups := 0
		for _, group := range m.groups {
			groupDone := true
			groupFailed := false
			for _, idx := range group.StepIndices {
				if m.steps[idx].Status == mergeStatusError {
					groupFailed = true
					break
				}
				if m.steps[idx].Status != mergeStatusDone {
					groupDone = false
				}
			}
			if groupFailed {
				failedGroups++
			} else if groupDone {
				completedGroups++
			}
		}
		b.WriteString("\n")
		if failedGroups > 0 {
			b.WriteString(m.styles.errorStyle.Render(fmt.Sprintf("Completed: %d, Failed: %d", completedGroups, failedGroups)))
		} else {
			b.WriteString(m.styles.doneStyle.Render(fmt.Sprintf("✓ All %d steps completed successfully", completedGroups)))
		}
		b.WriteString("\n")
	}

	return b.String()
}

func (m MergeTUIModel) renderCheckIndicators(checks []github.CheckDetail) string {
	var b strings.Builder
	b.WriteString("[")
	for _, check := range checks {
		var symbol string
		var style lipgloss.Style
		switch check.Status {
		case statusCompleted:
			symbol = dotSymbol
			switch check.Conclusion {
			case "SUCCESS":
				style = m.styles.doneStyle
			case "NEUTRAL", "SKIPPED":
				style = m.styles.dimStyle
			default:
				style = m.styles.errorStyle
			}
		case statusQueued:
			symbol = "○"
			style = m.styles.dimStyle
		case statusInProgress:
			symbol = dotSymbol
			style = m.styles.waitStyle
		default:
			symbol = "?"
			style = m.styles.dimStyle
		}
		b.WriteString(style.Render(symbol))
	}
	b.WriteString("]")
	return b.String()
}

func (m MergeTUIModel) renderProgressBar(elapsed, estimate time.Duration) string {
	width := 10
	if estimate == 0 {
		return ""
	}
	progress := float64(elapsed) / float64(estimate)
	if progress > 1.0 {
		progress = 1.0
	}
	filled := int(progress * float64(width))

	var b strings.Builder
	b.WriteString("[")
	for i := 0; i < width; i++ {
		if i < filled {
			b.WriteString(m.styles.doneStyle.Render("█"))
		} else {
			b.WriteString(m.styles.dimStyle.Render("░"))
		}
	}
	b.WriteString("]")
	return b.String()
}

func (m MergeTUIModel) renderDetailedChecks(checks []github.CheckDetail) string {
	if len(checks) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("    └ ")
	var activeChecks []string
	for _, check := range checks {
		if check.Status == statusInProgress || (check.Status == statusCompleted && check.Conclusion != "SUCCESS" && check.Conclusion != "NEUTRAL" && check.Conclusion != "SKIPPED") {
			status := "running"
			style := m.styles.waitStyle
			if check.Status == statusCompleted {
				status = strings.ToLower(check.Conclusion)
				style = m.styles.errorStyle
			}
			activeChecks = append(activeChecks, fmt.Sprintf("%s (%s)", check.Name, style.Render(status)))
		}
	}
	if len(activeChecks) == 0 {
		b.WriteString(m.styles.dimStyle.Render("waiting for checks to start..."))
	} else {
		b.WriteString(strings.Join(activeChecks, ", "))
	}
	b.WriteString("\n")
	return b.String()
}

// ProgressUpdate represents an update to merge progress
type ProgressUpdate struct {
	Type              string // "started", "completed", "failed", "waiting", "estimate"
	StepIndex         int
	Description       string
	Error             error
	Elapsed           time.Duration
	Timeout           time.Duration
	Checks            []github.CheckDetail
	EstimatedDuration time.Duration
}

// RunMergeTUI runs the merge TUI with channel-based updates
func RunMergeTUI(groups []MergeGroup, stepDescriptions []string, updates <-chan ProgressUpdate, done chan<- bool) error {
	if !IsTTY() {
		return fmt.Errorf("RunMergeTUI called in non-interactive mode")
	}
	m := NewMergeTUIModel(groups, stepDescriptions)
	m.updates = updates
	m.doneChan = done

	// Create a program
	program := tea.NewProgram(m, tea.WithInput(os.Stdin), tea.WithOutput(os.Stdout))

	_, err := program.Run()

	// Signal completion after the program has finished and restored the terminal
	if done != nil {
		select {
		case done <- true:
		default:
		}
	}

	return err
}
