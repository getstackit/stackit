package branch

import (
	"fmt"
	"strings"

	"github.com/getstackit/stackit/internal/actions"
	"github.com/getstackit/stackit/internal/cli/common"
	"github.com/getstackit/stackit/internal/handlers"
	"github.com/getstackit/stackit/internal/output"
	"github.com/getstackit/stackit/internal/tui"
	"github.com/getstackit/stackit/internal/tui/style"
)

// NewGetUI creates a runner and handler pair for get operations.
// The runner manages terminal state; the handler processes events.
// Caller must defer runner.Cleanup() to restore terminal on exit.
// Currently returns nil runner as there's no TUI component yet.
func NewGetUI(out output.Output, _ output.Logger) (*tui.Runner, actions.GetHandler) {
	// TODO: Add interactive TUI handler when needed
	// For now, use simple handler for both TTY and non-TTY
	return nil, NewSimpleGetHandler(out)
}

// SimpleGetHandler provides streaming text output for non-TTY environments
type SimpleGetHandler struct {
	common.BaseHandler
	currentPhase actions.GetPhase
	targetBranch string
	prNumber     *int
}

// NewSimpleGetHandler creates a new SimpleGetHandler
func NewSimpleGetHandler(out output.Output) *SimpleGetHandler {
	return &SimpleGetHandler{
		BaseHandler: common.NewBaseHandler(out),
	}
}

// Start is called at the beginning of get
func (h *SimpleGetHandler) Start(targetBranch string, prNumber *int) {
	h.Lock()
	defer h.Unlock()
	h.targetBranch = targetBranch
	h.prNumber = prNumber
}

// EmitEvent handles progress updates
func (h *SimpleGetHandler) EmitEvent(event actions.GetEvent) {
	h.Lock()
	defer h.Unlock()

	// Handle phase transitions
	if event.Type == actions.GetEventStarted && event.Phase != h.currentPhase {
		h.currentPhase = event.Phase
		h.printPhaseHeader(event.Phase)
		return
	}

	// Handle progress events
	h.printEventLine(event)
}

// Complete is called when get finishes
func (h *SimpleGetHandler) Complete(summary actions.GetSummary) {
	h.Lock()
	defer h.Unlock()

	// Print blank line before summary
	h.Output.Newline()

	// Handle up-to-date case
	if summary.UpToDate {
		h.Output.Info("✨ Everything is up to date!")
		return
	}

	// Print summary parts
	parts := []string{}
	if summary.BranchesCreated > 0 {
		parts = append(parts, fmt.Sprintf("synced %d new", summary.BranchesCreated))
	}
	if summary.BranchesUpdated > 0 {
		parts = append(parts, fmt.Sprintf("updated %d", summary.BranchesUpdated))
	}
	if summary.Restacked > 0 {
		parts = append(parts, fmt.Sprintf("restacked %d", summary.Restacked))
	}

	if len(parts) > 0 {
		h.Output.Info("✅ Summary: %s", strings.Join(parts, ", "))
	}

	// Checkout message
	h.Output.Info("Checked out %s", style.ColorCurrentBranch(summary.TargetBranch))

	// Status messages
	if summary.IsFrozen {
		h.Output.Info("Branch %s was retrieved in 'frozen' mode (local-only), making it uneditable",
			style.ColorBranchName(summary.TargetBranch))
		h.Output.Info("Use %s to make it editable", style.ColorCyan("st unfreeze"))
	}
}

func (h *SimpleGetHandler) printPhaseHeader(phase actions.GetPhase) {
	// Add spacing between phases (but not before first phase)
	if h.currentPhase != "" && phase != actions.GetPhaseFetch {
		h.Output.Newline()
	}

	switch phase {
	case actions.GetPhaseFetch:
		h.Output.Info("📥 Fetching from remote...")
	case actions.GetPhaseSync:
		h.Output.Info("🔄 Syncing branches...")
	case actions.GetPhaseMetadata:
		// Metadata phase is silent
	case actions.GetPhaseCheckout:
		// Checkout is handled in Complete
	}
}

func (h *SimpleGetHandler) printEventLine(event actions.GetEvent) {
	switch event.Phase {
	case actions.GetPhaseFetch:
		h.printFetchEvent(event)
	case actions.GetPhaseSync:
		h.printSyncEvent(event)
	}
}

func (h *SimpleGetHandler) printFetchEvent(event actions.GetEvent) {
	if event.Type == actions.GetEventCompleted {
		if event.NewRevision != "" {
			h.Output.Info("  %s fast-forwarded to %s",
				style.ColorBranchName(event.Branch),
				style.ColorDim(event.NewRevision))
		} else {
			h.Output.Info("  %s is up to date", style.ColorBranchName(event.Branch))
		}
	}
}

func (h *SimpleGetHandler) printSyncEvent(event actions.GetEvent) {
	if event.Type != actions.GetEventCompleted {
		return
	}

	prInfo := common.FormatPRInfo(event.PRNumber)

	if event.IsNew {
		h.Output.Info("  Synced %s%s from remote",
			style.ColorBranchName(event.Branch),
			prInfo)
	} else {
		h.Output.Info("  Updated %s%s from remote",
			style.ColorBranchName(event.Branch),
			prInfo)
	}
}

// OnRestackStart implements RestackHandler for restack phase
func (h *SimpleGetHandler) OnRestackStart(_ int) {
	h.Lock()
	defer h.Unlock()
	h.Output.Newline()
	h.Output.Info("📚 Restacking branches...")
}

// OnRestackBranch implements RestackHandler for restack phase
func (h *SimpleGetHandler) OnRestackBranch(event handlers.RestackBranchEvent) {
	h.Lock()
	defer h.Unlock()

	if event.Reparented {
		h.Output.Info("  Reparented %s from %s to %s",
			style.ColorBranchNameIf(event.Branch, event.IsCurrent),
			style.ColorBranchName(event.OldParent),
			style.ColorBranchName(event.NewParent))
	}

	prInfo := common.FormatPRInfo(event.PRNumber)

	switch event.Result {
	case handlers.RestackDone:
		msg := fmt.Sprintf("Restacked %s%s", style.ColorBranchNameIf(event.Branch, event.IsCurrent), prInfo)
		if event.Parent != "" {
			msg += fmt.Sprintf(" on %s", style.ColorBranchName(event.Parent))
		}
		msg += fmt.Sprintf(" -> %s", style.ColorDim(event.NewRevision))
		h.Output.Info("  %s", msg)
		if event.RerereResolvedCount > 0 {
			h.Output.Info("%s", actions.FormatRerereResolved(event.RerereResolvedCount))
		}
	case handlers.RestackUnneeded:
		reason := common.ReasonNoRestackNeeded
		if event.LockReason.IsLocked() {
			reason = fmt.Sprintf("%s: %s", common.ReasonLocked, event.LockReason)
		} else if event.Frozen {
			reason = common.ReasonFrozen
		}

		msg := fmt.Sprintf("%s%s %s", style.ColorBranchNameIf(event.Branch, event.IsCurrent), prInfo, reason)
		if reason == common.ReasonNoRestackNeeded {
			msg = fmt.Sprintf("%s%s up to date", style.ColorBranchNameIf(event.Branch, event.IsCurrent), prInfo)
		}
		h.Output.Info("  %s", msg)
	case handlers.RestackConflict:
		h.Output.Warn("  Skipped %s%s (conflict)",
			style.ColorBranchNameIf(event.Branch, event.IsCurrent),
			prInfo)
	case handlers.RestackBlocked:
		h.Output.Warn("  Skipped %s%s (blocked by conflict in stack)",
			style.ColorBranchNameIf(event.Branch, event.IsCurrent),
			prInfo)
	}
}

// OnRestackComplete implements RestackHandler for restack phase
func (h *SimpleGetHandler) OnRestackComplete(summary handlers.RestackSummary) {
	h.Lock()
	defer h.Unlock()

	if summary.Restacked == 0 && summary.Skipped == 0 && len(summary.Blocked) == 0 {
		return // No restack summary needed if nothing happened
	}

	parts := []string{}
	if summary.Restacked > 0 {
		parts = append(parts, fmt.Sprintf("restacked %d", summary.Restacked))
	}
	if summary.Skipped > 0 {
		parts = append(parts, fmt.Sprintf("skipped %d (conflict)", summary.Skipped))
	}
	if len(summary.Blocked) > 0 {
		parts = append(parts, fmt.Sprintf("blocked %d", len(summary.Blocked)))
	}

	if len(parts) > 0 {
		h.Output.Info("  %s", strings.Join(parts, ", "))
	}

	for _, conflict := range summary.Conflicts {
		h.Output.Info("  Run %s to resolve and continue",
			style.ColorCyan(fmt.Sprintf("st restack %s", conflict)))
	}
}
