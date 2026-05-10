// Package merge provides a TUI component for displaying merge progress.
package merge

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/getstackit/stackit/internal/github"
	"github.com/getstackit/stackit/internal/tui/core"
	"github.com/getstackit/stackit/internal/tui/style"
)

// Group represents a group of steps that should be displayed as a single line
type Group struct {
	Label       string
	StepIndices []int
}

// StepStatus represents the status of a merge step
type StepStatus string

// Step status constants
const (
	StatusPending StepStatus = "pending"
	StatusRunning StepStatus = "running"
	StatusWaiting StepStatus = "waiting"
	StatusDone    StepStatus = "done"
	StatusError   StepStatus = "error"
)

// StepItem represents a step in the merge process
type StepItem struct {
	StepIndex   int
	Description string
	Status      StepStatus
	Error       error
	WaitElapsed time.Duration
	WaitTimeout time.Duration
	Checks      []github.CheckDetail
}

// Model is the bubbletea model for merge progress
// It embeds core.BaseModel for standard lifecycle handling.
type Model struct {
	core.BaseModel    // Embedded for ReadySignaler interface
	Groups            []Group
	Steps             []StepItem
	CurrentIdx        int
	spinner           spinner.Model // lowercase for custom style
	Quitting          bool          // unique to merge, used in View()
	styles            styles
	EstimatedDuration time.Duration
	Summary           string
}

// styles holds the visual styling for the merge component.
// Uses shared styles from internal/tui/style for consistency.
type styles struct {
	spinnerStyle lipgloss.Style
	doneStyle    lipgloss.Style
	errorStyle   lipgloss.Style
	waitStyle    lipgloss.Style
	dimStyle     lipgloss.Style
	timeStyle    lipgloss.Style
}

// newStyles creates styles using the shared style definitions.
func newStyles() styles {
	statusStyles := style.DefaultStatusStyles()
	commonStyles := style.DefaultCommonStyles()
	return styles{
		spinnerStyle: commonStyles.Spinner,
		doneStyle:    statusStyles.Done,
		errorStyle:   statusStyles.Error,
		waitStyle:    statusStyles.Warning,
		dimStyle:     commonStyles.Subtle,
		timeStyle:    commonStyles.Dim,
	}
}

const (
	dotSymbol        = "●"
	statusCompleted  = "COMPLETED"
	statusInProgress = "IN_PROGRESS"
	statusQueued     = "QUEUED"
)

// Message types for the merge component

// PlanLoadedMsg indicates the merge plan has been loaded
type PlanLoadedMsg struct {
	Groups           []Group
	StepDescriptions []string
}

// StepStartMsg indicates a step has started
type StepStartMsg struct {
	StepIndex   int
	Description string
}

// StepCompleteMsg indicates a step has completed
type StepCompleteMsg struct {
	StepIndex int
}

// StepFailedMsg indicates a step has failed
type StepFailedMsg struct {
	StepIndex int
	Error     error
}

// StepWaitingMsg indicates a step is waiting for CI
type StepWaitingMsg struct {
	StepIndex int
	Elapsed   time.Duration
	Timeout   time.Duration
	Checks    []github.CheckDetail
}

// EstimatedDurationMsg updates the estimated duration
type EstimatedDurationMsg time.Duration

// CompleteMsg indicates the merge is complete
type CompleteMsg struct {
	Summary string
}

// NewModel creates a new merge model
func NewModel() *Model {
	styles := newStyles()

	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = styles.spinnerStyle

	return &Model{
		Groups:     []Group{},
		Steps:      []StepItem{},
		CurrentIdx: 0,
		spinner:    s,
		styles:     styles,
	}
}

// Init initializes the bubbletea model.
func (m *Model) Init() tea.Cmd {
	// Signal that the program is ready to receive messages via BaseModel
	m.SignalReady()
	return m.spinner.Tick
}

// Update handles message updates for the bubbletea model
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Handle quit keys first to set Quitting flag (unique to merge component)
	if keyMsg, ok := msg.(tea.KeyPressMsg); ok {
		if keyMsg.String() == core.KeyCtrlC || keyMsg.String() == core.KeyQuit {
			m.Quitting = true
			return m, tea.Quit
		}
	}

	// Handle spinner ticks with our custom spinner BEFORE HandleCommonMsg
	if tickMsg, ok := msg.(spinner.TickMsg); ok {
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(tickMsg)
		return m, cmd
	}

	// Handle window resize via BaseModel (key events already handled above)
	if sizeMsg, ok := msg.(tea.WindowSizeMsg); ok {
		m.Width = sizeMsg.Width
		m.Height = sizeMsg.Height
		return m, nil
	}

	switch msg := msg.(type) {
	case PlanLoadedMsg:
		m.Groups = msg.Groups
		m.Steps = make([]StepItem, len(msg.StepDescriptions))
		for i, desc := range msg.StepDescriptions {
			m.Steps[i] = StepItem{
				StepIndex:   i,
				Description: desc,
				Status:      StatusPending,
			}
		}
		// If no groups provided, create one group per step
		if len(m.Groups) == 0 {
			m.Groups = make([]Group, len(msg.StepDescriptions))
			for i, desc := range msg.StepDescriptions {
				m.Groups[i] = Group{
					Label:       desc,
					StepIndices: []int{i},
				}
			}
		}
		return m, nil

	case StepStartMsg:
		if msg.StepIndex < len(m.Steps) {
			m.Steps[msg.StepIndex].Status = StatusRunning
			m.CurrentIdx = msg.StepIndex
		}
		return m, nil

	case StepCompleteMsg:
		if msg.StepIndex < len(m.Steps) {
			m.Steps[msg.StepIndex].Status = StatusDone
			// Move to next step
			if msg.StepIndex == m.CurrentIdx {
				m.CurrentIdx++
				if m.CurrentIdx == len(m.Steps) {
					m.Done = true
				}
			}
		}
		return m, nil

	case StepFailedMsg:
		if msg.StepIndex < len(m.Steps) {
			m.Steps[msg.StepIndex].Status = StatusError
			m.Steps[msg.StepIndex].Error = msg.Error
			m.Done = true
		}
		return m, nil

	case StepWaitingMsg:
		if msg.StepIndex < len(m.Steps) {
			m.Steps[msg.StepIndex].Status = StatusWaiting
			m.Steps[msg.StepIndex].WaitElapsed = msg.Elapsed
			m.Steps[msg.StepIndex].WaitTimeout = msg.Timeout
			m.Steps[msg.StepIndex].Checks = msg.Checks
		}
		return m, nil

	case EstimatedDurationMsg:
		m.EstimatedDuration = time.Duration(msg)
		return m, nil

	case CompleteMsg:
		m.Done = true
		m.Summary = msg.Summary
		return m, tea.Quit
	}

	return m, nil
}

// View renders the TUI
func (m *Model) View() tea.View {
	if m.Quitting {
		return tea.NewView("")
	}

	var b strings.Builder
	b.WriteString("\n")

	totalGroups := len(m.Groups)

	// Handle empty plan - show initializing message
	if totalGroups == 0 {
		header := lipgloss.NewStyle().Bold(true).Render("Merge Progress")
		if m.Done {
			b.WriteString(header + m.styles.doneStyle.Render("  Complete"))
		} else {
			b.WriteString(header + "  " + m.spinner.View() + " " + m.styles.dimStyle.Render("Initializing..."))
		}
		b.WriteString("\n")
		if m.Summary != "" {
			b.WriteString("\n")
			b.WriteString(m.Summary)
			b.WriteString("\n")
		}
		return tea.NewView(b.String())
	}

	// Calculate overall progress
	completedGroups := 0
	currentGroupIdx := -1
	for i, group := range m.Groups {
		allDone := true
		for _, idx := range group.StepIndices {
			if idx < len(m.Steps) && m.Steps[idx].Status != StatusDone {
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
	if m.Done {
		progressIndicator = m.styles.doneStyle.Render(fmt.Sprintf("  %d of %d complete", completedGroups, totalGroups))
	}
	b.WriteString(header + progressIndicator)
	b.WriteString("\n\n")

	// Categorize groups by status
	type groupInfo struct {
		index      int
		status     StepStatus
		activeStep *StepItem
		failedStep *StepItem
	}

	var completedGroupInfos []groupInfo
	var runningGroupInfo *groupInfo
	var pendingGroupInfos []groupInfo

	for i, group := range m.Groups {
		var status StepStatus
		var activeStep *StepItem
		var failedStep *StepItem

		allDone := true
		allPending := true

		for _, idx := range group.StepIndices {
			if idx >= len(m.Steps) {
				continue
			}
			step := &m.Steps[idx]
			if step.Status == StatusError {
				failedStep = step
				status = StatusError
				break
			}
			if step.Status != StatusDone {
				allDone = false
			}
			if step.Status != StatusPending {
				allPending = false
			}
			if (step.Status == StatusRunning || step.Status == StatusWaiting) && activeStep == nil {
				activeStep = step
			}
		}

		if status != StatusError {
			switch {
			case allDone:
				status = StatusDone
			case allPending:
				status = StatusPending
			default:
				status = StatusRunning
			}
		}

		info := groupInfo{index: i, status: status, activeStep: activeStep, failedStep: failedStep}
		switch status {
		case StatusDone:
			completedGroupInfos = append(completedGroupInfos, info)
		case StatusRunning, StatusError:
			runningGroupInfo = &info
		case StatusPending:
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
		group := m.Groups[info.index]
		fmt.Fprintf(&b, "  %s %s\n", m.styles.doneStyle.Render("✓"), m.styles.doneStyle.Render(group.Label))
	}

	// Show current running/error group with full details
	if runningGroupInfo != nil {
		group := m.Groups[runningGroupInfo.index]
		var line strings.Builder

		if runningGroupInfo.status == StatusError {
			fmt.Fprintf(&line, "  %s %s ", m.styles.errorStyle.Render("✗"), m.styles.errorStyle.Render(group.Label))
			if runningGroupInfo.failedStep != nil && runningGroupInfo.failedStep.Error != nil {
				line.WriteString(m.styles.errorStyle.Render("→ " + runningGroupInfo.failedStep.Error.Error()))
			}
		} else {
			fmt.Fprintf(&line, "  %s %s ", m.spinner.View(), lipgloss.NewStyle().Bold(true).Render(group.Label))

			if runningGroupInfo.activeStep != nil {
				if runningGroupInfo.activeStep.Status == StatusWaiting {
					elapsed := runningGroupInfo.activeStep.WaitElapsed.Round(time.Second)

					if len(runningGroupInfo.activeStep.Checks) > 0 {
						line.WriteString(m.renderCheckIndicators(runningGroupInfo.activeStep.Checks))
						line.WriteString(" ")
					}

					if m.EstimatedDuration > 0 {
						line.WriteString(m.renderProgressBar(elapsed, m.EstimatedDuration))
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
		group := m.Groups[info.index]
		fmt.Fprintf(&b, "  %s %s\n", m.styles.dimStyle.Render("○"), m.styles.dimStyle.Render(group.Label))
	}

	// Show ellipsis if we're hiding pending groups
	if len(pendingGroupInfos) > maxPendingToShow {
		remaining := len(pendingGroupInfos) - maxPendingToShow
		if remaining == 1 {
			b.WriteString(m.styles.dimStyle.Render("  ... 1 more step\n"))
		} else {
			b.WriteString(m.styles.dimStyle.Render(fmt.Sprintf("  ... %d more steps\n", remaining)))
		}
	}

	if m.Done {
		completedGroups := 0
		failedGroups := 0
		for _, group := range m.Groups {
			groupDone := true
			groupFailed := false
			for _, idx := range group.StepIndices {
				if idx >= len(m.Steps) {
					continue
				}
				if m.Steps[idx].Status == StatusError {
					groupFailed = true
					break
				}
				if m.Steps[idx].Status != StatusDone {
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

	// Show summary if available
	if m.Summary != "" {
		b.WriteString("\n")
		b.WriteString(m.Summary)
		b.WriteString("\n")
	}

	return tea.NewView(b.String())
}

func (m *Model) renderCheckIndicators(checks []github.CheckDetail) string {
	var b strings.Builder
	b.WriteString("[")
	for _, check := range checks {
		var symbol string
		var s lipgloss.Style
		switch check.Status {
		case statusCompleted:
			symbol = dotSymbol
			switch check.Conclusion {
			case "SUCCESS":
				s = m.styles.doneStyle
			case "NEUTRAL", "SKIPPED":
				s = m.styles.dimStyle
			default:
				s = m.styles.errorStyle
			}
		case statusQueued:
			symbol = "○"
			s = m.styles.dimStyle
		case statusInProgress:
			symbol = dotSymbol
			s = m.styles.waitStyle
		default:
			symbol = "?"
			s = m.styles.dimStyle
		}
		_, _ = b.WriteString(s.Render(symbol))
	}
	b.WriteString("]")
	return b.String()
}

func (m *Model) renderProgressBar(elapsed, estimate time.Duration) string {
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
	for i := range width {
		if i < filled {
			_, _ = b.WriteString(m.styles.doneStyle.Render("█"))
		} else {
			_, _ = b.WriteString(m.styles.dimStyle.Render("░"))
		}
	}
	b.WriteString("]")
	return b.String()
}

func (m *Model) renderDetailedChecks(checks []github.CheckDetail) string {
	if len(checks) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("    └ ")
	var activeChecks []string
	for _, check := range checks {
		if check.Status == statusInProgress || (check.Status == statusCompleted && check.Conclusion != "SUCCESS" && check.Conclusion != "NEUTRAL" && check.Conclusion != "SKIPPED") {
			status := "running"
			s := m.styles.waitStyle
			if check.Status == statusCompleted {
				status = strings.ToLower(check.Conclusion)
				s = m.styles.errorStyle
			}
			activeChecks = append(activeChecks, fmt.Sprintf("%s (%s)", check.Name, s.Render(status)))
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
