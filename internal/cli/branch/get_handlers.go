package branch

import (
	"errors"
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

// describeLandedAncestors writes the human-readable explanation of branches
// re-anchored past landed work. Split from the prompt so the text is identical
// whether or not there is a terminal to ask on.
func describeLandedAncestors(out output.Output, report actions.LandedAncestorReport) {
	out.Newline()
	out.Warn("Landed work in this stack is no longer on the remote:")
	for _, r := range report.Reanchored {
		out.Info("  • %s%s has landed; %s is now tracked against %s",
			style.ColorBranchName(r.LandedParent),
			common.FormatPRInfo(r.LandedPR),
			style.ColorBranchName(r.Branch),
			style.ColorBranchName(r.NewParent))
	}

	// Re-anchoring moved the parent pointer, not the commits. Say so plainly:
	// a frozen branch mirrors the remote, and restack resets it instead of
	// rebasing it, so its diff keeps the landed commits until it is unfrozen.
	if unfreezable := report.Unfreezable(); len(unfreezable) > 0 {
		out.Newline()
		out.Info("  %s still %s the landed commits. Frozen branches mirror the remote, so restack won't rebase %s.",
			formatBranchList(unfreezable),
			pluralWord(len(unfreezable), "contains", "contain"),
			actions.PluralIt(len(unfreezable) != 1))
	}

	// Without the parent tip a branch was pushed on top of, there is nothing to
	// separate the landed commits from the branch's own, and a rebase replays
	// them rather than dropping them. Staying frozen is the only safe answer,
	// so say that rather than pointing at a remedy that would do damage.
	if unanchored := report.Unanchored(); len(unanchored) > 0 {
		out.Newline()
		out.Info("  No record on the remote of the parent tip for %s, so the landed commits can't be separated from %s own work.",
			formatBranchList(unanchored),
			pluralWord(len(unanchored), "its", "their"))
		out.Info("  Left frozen — rebasing by hand would replay the landed work.")
	}
}

// ReportLandedAncestors implements GetHandler. Explains the re-anchoring and,
// when there is something safe to offer and a terminal to ask on, offers to
// unfreeze the affected branches so this run rebases them.
func (h *SimpleGetHandler) ReportLandedAncestors(report actions.LandedAncestorReport) (actions.LandedAncestorDecision, error) {
	h.Lock()
	defer h.Unlock()

	describeLandedAncestors(h.Output, report)

	unfreezable := report.Unfreezable()
	if len(unfreezable) == 0 || !report.CanRestack {
		return actions.LeaveFrozen, nil
	}

	// Unstyled, and naming the branches rather than a parent: more than one
	// ancestor can have landed, and then they do not share a new parent.
	confirmed, err := tui.PromptConfirm(
		fmt.Sprintf("Unfreeze and restack %s now?", strings.Join(unfreezable, ", ")), false)
	if err != nil && !errors.Is(err, tui.ErrInteractiveDisabled) {
		return actions.LeaveFrozen, err
	}
	if confirmed {
		return actions.UnfreezeAndRestack, nil
	}

	// `st unfreeze` takes one branch and thaws its upstack with it, so naming
	// the trunk-most branch covers the rest of the subtree.
	h.Output.Info("  Run %s then %s when you're ready to rebase.",
		style.ColorCyan("st unfreeze "+unfreezable[0]),
		style.ColorCyan("st restack"))
	return actions.LeaveFrozen, nil
}

// pluralWord picks the singular or plural form for a count of branches.
func pluralWord(count int, singular, plural string) string {
	if count == 1 {
		return singular
	}
	return plural
}

// formatBranchList renders branch names for prose: "a", "a and b", "a, b and c".
func formatBranchList(names []string) string {
	styled := make([]string, len(names))
	for i, name := range names {
		styled[i] = style.ColorBranchName(name)
	}
	switch len(styled) {
	case 0:
		return ""
	case 1:
		return styled[0]
	default:
		return strings.Join(styled[:len(styled)-1], ", ") + " and " + styled[len(styled)-1]
	}
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
		// A held branch reports the same status as one that needed no work, so
		// warn instead of quietly claiming it is up to date.
		if event.HeldBy != "" {
			h.Output.Warn("  Held %s%s back: %s",
				style.ColorBranchNameIf(event.Branch, event.IsCurrent), prInfo, event.HeldBy)
			return
		}

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
