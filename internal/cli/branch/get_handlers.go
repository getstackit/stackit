package branch

import (
	"fmt"
	"strings"
	stdsync "sync"

	"stackit.dev/stackit/internal/actions"
	"stackit.dev/stackit/internal/engine"
	"stackit.dev/stackit/internal/handlers"
	"stackit.dev/stackit/internal/output"
	"stackit.dev/stackit/internal/tui"
	"stackit.dev/stackit/internal/tui/style"
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
	splog        output.Output
	currentPhase actions.GetPhase
	mu           stdsync.Mutex
	targetBranch string
	prNumber     *int
}

// NewSimpleGetHandler creates a new SimpleGetHandler
func NewSimpleGetHandler(splog output.Output) *SimpleGetHandler {
	return &SimpleGetHandler{
		splog: splog,
	}
}

// Start is called at the beginning of get
func (h *SimpleGetHandler) Start(targetBranch string, prNumber *int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.targetBranch = targetBranch
	h.prNumber = prNumber
}

// EmitEvent handles progress updates
func (h *SimpleGetHandler) EmitEvent(event actions.GetEvent) {
	h.mu.Lock()
	defer h.mu.Unlock()

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
	h.mu.Lock()
	defer h.mu.Unlock()

	// Print blank line before summary
	h.splog.Newline()

	// Handle up-to-date case
	if summary.UpToDate {
		h.splog.Info("✨ Everything is up to date!")
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
		h.splog.Info("✅ Summary: %s", strings.Join(parts, ", "))
	}

	// Checkout message
	h.splog.Info("Checked out %s", style.ColorBranchName(summary.TargetBranch, true))

	// Status messages
	if summary.IsFrozen {
		h.splog.Info("Branch %s was retrieved in 'frozen' mode (local-only), making it uneditable",
			style.ColorBranchName(summary.TargetBranch, false))
		h.splog.Info("Use %s to make it editable", style.ColorCyan("st unfreeze"))
	}
}

func (h *SimpleGetHandler) printPhaseHeader(phase actions.GetPhase) {
	// Add spacing between phases (but not before first phase)
	if h.currentPhase != "" && phase != actions.GetPhaseFetch {
		h.splog.Newline()
	}

	switch phase {
	case actions.GetPhaseFetch:
		h.splog.Info("📥 Fetching from remote...")
	case actions.GetPhaseSync:
		h.splog.Info("🔄 Syncing branches...")
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
			h.splog.Info("  %s fast-forwarded to %s",
				style.ColorBranchName(event.Branch, false),
				style.ColorDim(event.NewRevision))
		} else {
			h.splog.Info("  %s is up to date", style.ColorBranchName(event.Branch, false))
		}
	}
}

func (h *SimpleGetHandler) printSyncEvent(event actions.GetEvent) {
	if event.Type != actions.GetEventCompleted {
		return
	}

	prInfo := h.formatPRInfo(event.PRNumber)

	if event.IsNew {
		h.splog.Info("  Synced %s%s from remote",
			style.ColorBranchName(event.Branch, false),
			prInfo)
	} else {
		h.splog.Info("  Updated %s%s from remote",
			style.ColorBranchName(event.Branch, false),
			prInfo)
	}
}

func (h *SimpleGetHandler) formatPRInfo(prNumber *int) string {
	if prNumber == nil {
		return ""
	}
	return fmt.Sprintf(" (PR #%d)", *prNumber)
}

// OnRestackStart implements RestackHandler for restack phase
func (h *SimpleGetHandler) OnRestackStart(_ int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.splog.Newline()
	h.splog.Info("📚 Restacking branches...")
}

// OnRestackBranch implements RestackHandler for restack phase
func (h *SimpleGetHandler) OnRestackBranch(branch string, result handlers.RestackResult, newRev string, prNumber *int, lockReason engine.LockReason, frozen bool, isCurrent bool, parent string, reparented bool, oldParent, newParent string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if reparented {
		h.splog.Info("  Reparented %s from %s to %s",
			style.ColorBranchName(branch, isCurrent),
			style.ColorBranchName(oldParent, false),
			style.ColorBranchName(newParent, false))
	}

	prInfo := h.formatPRInfo(prNumber)

	switch result {
	case handlers.RestackDone:
		msg := fmt.Sprintf("Restacked %s%s", style.ColorBranchName(branch, isCurrent), prInfo)
		if parent != "" {
			msg += fmt.Sprintf(" on %s", style.ColorBranchName(parent, false))
		}
		msg += fmt.Sprintf(" -> %s", style.ColorDim(newRev))
		h.splog.Info("  %s", msg)
	case handlers.RestackUnneeded:
		reason := "does not need restacking"
		if lockReason.IsLocked() {
			reason = fmt.Sprintf("is locked: %s", lockReason)
		} else if frozen {
			reason = "is frozen"
		}

		msg := fmt.Sprintf("%s%s %s", style.ColorBranchName(branch, isCurrent), prInfo, reason)
		if reason == "does not need restacking" && parent != "" {
			msg = fmt.Sprintf("%s%s does not need to be restacked on %s.",
				style.ColorBranchName(branch, isCurrent),
				prInfo,
				style.ColorBranchName(parent, false))
		}
		h.splog.Info("  %s", msg)
	case handlers.RestackConflict:
		h.splog.Warn("  Skipped %s%s (conflict)",
			style.ColorBranchName(branch, isCurrent),
			prInfo)
	}
}

// OnRestackComplete implements RestackHandler for restack phase
func (h *SimpleGetHandler) OnRestackComplete(restacked, skipped int, conflicts []string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if restacked == 0 && skipped == 0 {
		return // No restack summary needed if nothing happened
	}

	parts := []string{}
	if restacked > 0 {
		parts = append(parts, fmt.Sprintf("restacked %d", restacked))
	}
	if skipped > 0 {
		parts = append(parts, fmt.Sprintf("skipped %d (conflict)", skipped))
	}

	if len(parts) > 0 {
		h.splog.Info("  %s", strings.Join(parts, ", "))
	}

	if len(conflicts) > 0 {
		h.splog.Info("  Run %s to resolve and continue",
			style.ColorCyan(fmt.Sprintf("st restack %s", conflicts[0])))
	}
}
