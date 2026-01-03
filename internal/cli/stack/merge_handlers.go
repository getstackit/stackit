package stack

import (
	"fmt"
	"time"

	"stackit.dev/stackit/internal/actions/merge"
	"stackit.dev/stackit/internal/app"
	"stackit.dev/stackit/internal/github"
	"stackit.dev/stackit/internal/tui"
)

// NewMergeHandler creates the appropriate handler based on TTY availability
func NewMergeHandler(ctx *app.Context) merge.Handler {
	if tui.IsTTY() {
		return NewInteractiveMergeHandler(ctx)
	}
	return NewSimpleMergeHandler(ctx)
}

// SimpleMergeHandler provides plain text output for merge operations
type SimpleMergeHandler struct {
	ctx *app.Context
}

// NewSimpleMergeHandler creates a new SimpleMergeHandler
func NewSimpleMergeHandler(ctx *app.Context) *SimpleMergeHandler {
	return &SimpleMergeHandler{ctx: ctx}
}

// Start implements merge.Handler.
func (h *SimpleMergeHandler) Start(_ *merge.Plan) {}

// StepStarted implements merge.Handler.
func (h *SimpleMergeHandler) StepStarted(_ int, description string) {
	h.ctx.Splog.Info("Starting: %s", description)
}

// StepCompleted implements merge.Handler.
func (h *SimpleMergeHandler) StepCompleted(_ int) {
	// Simple completion message handled by next step or final summary
}

// StepFailed implements merge.Handler.
func (h *SimpleMergeHandler) StepFailed(_ int, err error) {
	h.ctx.Splog.Error("Step failed: %v", err)
}

// StepWaiting implements merge.Handler.
func (h *SimpleMergeHandler) StepWaiting(_ int, elapsed, _ time.Duration, _ []github.CheckDetail) {
	if int(elapsed.Seconds())%30 == 0 {
		h.ctx.Splog.Info("  ... still waiting (%v elapsed)", elapsed.Round(time.Second))
	}
}

// SetEstimatedDuration implements merge.Handler.
func (h *SimpleMergeHandler) SetEstimatedDuration(_ time.Duration) {}

// Complete implements merge.Handler.
func (h *SimpleMergeHandler) Complete(result *merge.ConsolidationResult) {
	if result != nil {
		h.ctx.Splog.Info("✅ Created consolidation PR #%d: %s", result.PRNumber, result.PRURL)
	}
}

// InteractiveMergeHandler provides a TUI for merge operations
type InteractiveMergeHandler struct {
	ctx      *app.Context
	reporter *tui.ChannelMergeProgressReporter
	done     chan bool
	errChan  chan error
	plan     *merge.Plan
}

// NewInteractiveMergeHandler creates a new InteractiveMergeHandler
func NewInteractiveMergeHandler(ctx *app.Context) *InteractiveMergeHandler {
	return &InteractiveMergeHandler{
		ctx: ctx,
	}
}

// Start implements merge.Handler.
func (h *InteractiveMergeHandler) Start(plan *merge.Plan) {
	h.startTUI(plan)
}

func (h *InteractiveMergeHandler) startTUI(plan *merge.Plan) {
	h.plan = plan
	h.reporter = tui.NewChannelMergeProgressReporter()
	h.done = make(chan bool, 1)
	h.errChan = make(chan error, 1)

	groups := CalculateMergeGroups(plan)
	stepDescriptions := make([]string, len(plan.Steps))
	for i, step := range plan.Steps {
		stepDescriptions[i] = step.Description
	}

	h.ctx.Splog.SetQuiet(true)

	go func() {
		err := tui.RunMergeTUI(groups, stepDescriptions, h.reporter.Updates(), h.done)
		if err != nil {
			h.errChan <- err
		}
	}()
}

// StepStarted implements merge.Handler.
func (h *InteractiveMergeHandler) StepStarted(stepIndex int, description string) {
	if h.reporter != nil {
		h.reporter.StepStarted(stepIndex, description)
	}
}

// StepCompleted implements merge.Handler.
func (h *InteractiveMergeHandler) StepCompleted(stepIndex int) {
	if h.reporter != nil {
		h.reporter.StepCompleted(stepIndex)
		if stepIndex == len(h.plan.Steps)-1 {
			h.reporter.Close()
			<-h.done
			h.ctx.Splog.SetQuiet(false)
		}
	}
}

// StepFailed implements merge.Handler.
func (h *InteractiveMergeHandler) StepFailed(stepIndex int, err error) {
	if h.reporter != nil {
		h.reporter.StepFailed(stepIndex, err)
		h.reporter.Close()
		<-h.done
		h.ctx.Splog.SetQuiet(false)
	}
}

// StepWaiting implements merge.Handler.
func (h *InteractiveMergeHandler) StepWaiting(stepIndex int, elapsed, timeout time.Duration, checks []github.CheckDetail) {
	if h.reporter != nil {
		h.reporter.StepWaiting(stepIndex, elapsed, timeout, checks)
	}
}

// SetEstimatedDuration implements merge.Handler.
func (h *InteractiveMergeHandler) SetEstimatedDuration(duration time.Duration) {
	if h.reporter != nil {
		h.reporter.SetEstimatedDuration(duration)
	}
}

// Complete implements merge.Handler.
func (h *InteractiveMergeHandler) Complete(result *merge.ConsolidationResult) {
	if result != nil {
		h.ctx.Splog.Info("✅ Created consolidation PR #%d: %s", result.PRNumber, result.PRURL)
	}
}

// CalculateMergeGroups calculates groups for the TUI
func CalculateMergeGroups(plan *merge.Plan) []tui.MergeGroup {
	var groups []tui.MergeGroup
	assigned := make(map[int]bool)

	// 1. Create groups for each branch being merged
	for _, branchInfo := range plan.BranchesToMerge {
		var indices []int
		for i, step := range plan.Steps {
			if step.BranchName == branchInfo.BranchName {
				indices = append(indices, i)
				assigned[i] = true
			}
		}
		if len(indices) > 0 {
			groups = append(groups, tui.MergeGroup{
				Label:       fmt.Sprintf("PR #%d (%s)", branchInfo.PRNumber, branchInfo.BranchName),
				StepIndices: indices,
			})
		}
	}

	// 2. Create group for upstack branches
	if len(plan.UpstackBranches) > 0 {
		var indices []int
		for i, step := range plan.Steps {
			if assigned[i] {
				continue
			}
			for _, ub := range plan.UpstackBranches {
				if step.BranchName == ub {
					indices = append(indices, i)
					assigned[i] = true
					break
				}
			}
		}
		if len(indices) > 0 {
			groups = append(groups, tui.MergeGroup{
				Label:       "Restack upstack branches",
				StepIndices: indices,
			})
		}
	}

	// 3. Remaining steps (like PullTrunk)
	for i := 0; i < len(plan.Steps); i++ {
		if assigned[i] {
			continue
		}

		label := plan.Steps[i].Description
		if plan.Steps[i].StepType == merge.StepPullTrunk {
			label = "Sync trunk"
		}

		groups = append(groups, tui.MergeGroup{
			Label:       label,
			StepIndices: []int{i},
		})
		assigned[i] = true
	}

	return groups
}
