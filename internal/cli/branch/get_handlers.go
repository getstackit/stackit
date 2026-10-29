package branch

import (
	"errors"
	"fmt"
	"strings"

	"github.com/getstackit/stackit/internal/term"

	"github.com/getstackit/stackit/internal/actions"
	"github.com/getstackit/stackit/internal/actions/get"
	"github.com/getstackit/stackit/internal/cli/common"
	"github.com/getstackit/stackit/internal/git"

	"github.com/getstackit/stackit/internal/output"
	"github.com/getstackit/stackit/internal/tui"
	"github.com/getstackit/stackit/internal/tui/style"
)

// NewGetUI creates a runner and handler pair for get operations.
// The runner manages terminal state; the handler processes events.
// Caller must defer runner.Cleanup() to restore terminal on exit.
// Currently returns nil runner as there's no TUI component yet.
func NewGetUI(out output.Output, _ output.Logger) (*tui.Runner, get.Handler) {
	// TODO: Add interactive TUI handler when needed
	// For now, use simple handler for both TTY and non-TTY
	return nil, NewSimpleGetHandler(out)
}

// SimpleGetHandler provides streaming text output for non-TTY environments
type SimpleGetHandler struct {
	common.BaseHandler
	currentPhase get.Phase
	targetBranch string
	prNumber     *git.PRNumber
}

// NewSimpleGetHandler creates a new SimpleGetHandler
func NewSimpleGetHandler(out output.Output) *SimpleGetHandler {
	return &SimpleGetHandler{
		BaseHandler: common.NewBaseHandler(out),
	}
}

// Start is called at the beginning of get
func (h *SimpleGetHandler) Start(targetBranch string, prNumber *git.PRNumber) {
	h.Lock()
	defer h.Unlock()
	h.targetBranch = targetBranch
	h.prNumber = prNumber
}

// EmitEvent handles progress updates
func (h *SimpleGetHandler) EmitEvent(event get.Event) {
	h.Lock()
	defer h.Unlock()

	// Handle phase transitions
	if event.Type == get.EventStarted && event.Phase != h.currentPhase {
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
func describeLandedAncestors(out output.Output, report get.LandedAncestorReport) {
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

// ReportLandedAncestors implements get.Handler. Explains the re-anchoring and,
// when there is something safe to offer and a terminal to ask on, offers to
// unfreeze the affected branches so this run rebases them.
func (h *SimpleGetHandler) ReportLandedAncestors(report get.LandedAncestorReport) (get.LandedAncestorDecision, error) {
	h.Lock()
	defer h.Unlock()

	describeLandedAncestors(h.Output, report)

	unfreezable := report.Unfreezable()
	if len(unfreezable) == 0 || !report.CanRestack {
		return get.LeaveFrozen, nil
	}

	// Unstyled, and naming the branches rather than a parent: more than one
	// ancestor can have landed, and then they do not share a new parent.
	confirmed, err := tui.PromptConfirm(
		fmt.Sprintf("Unfreeze and restack %s now?", strings.Join(unfreezable, ", ")), false)
	if err != nil && !errors.Is(err, term.ErrInteractiveDisabled) {
		return get.LeaveFrozen, err
	}
	if confirmed {
		return get.UnfreezeAndRestack, nil
	}

	// `st unfreeze` takes one branch and thaws its upstack with it, so naming
	// the trunk-most branch covers the rest of the subtree.
	h.Output.Info("  Run %s then %s when you're ready to rebase.",
		style.ColorCyan("st unfreeze "+unfreezable[0]),
		style.ColorCyan("st restack"))
	return get.LeaveFrozen, nil
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
func (h *SimpleGetHandler) Complete(summary get.Summary) {
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

func (h *SimpleGetHandler) printPhaseHeader(phase get.Phase) {
	// Add spacing between phases (but not before first phase)
	if h.currentPhase != "" && phase != get.PhaseFetch {
		h.Output.Newline()
	}

	switch phase {
	case get.PhaseFetch:
		h.Output.Info("📥 Fetching from remote...")
	case get.PhaseSync:
		h.Output.Info("🔄 Syncing branches...")
	case get.PhaseMetadata:
		// Metadata phase is silent
	case get.PhaseCheckout:
		// Checkout is handled in Complete
	}
}

func (h *SimpleGetHandler) printEventLine(event get.Event) {
	switch event.Phase {
	case get.PhaseFetch:
		h.printFetchEvent(event)
	case get.PhaseSync:
		h.printSyncEvent(event)
	}
}

func (h *SimpleGetHandler) printFetchEvent(event get.Event) {
	if event.Type == get.EventCompleted {
		if event.NewRevision != "" {
			h.Output.Info("  %s fast-forwarded to %s",
				style.ColorBranchName(event.Branch),
				style.ColorDim(event.NewRevision))
		} else {
			h.Output.Info("  %s is up to date", style.ColorBranchName(event.Branch))
		}
	}
}

func (h *SimpleGetHandler) printSyncEvent(event get.Event) {
	// A skipped branch is the one case where saying nothing is wrong: the
	// branch silently keeps its old content, and the remedy lives in another
	// worktree the user is not looking at.
	if event.Type == get.EventSkipped {
		h.Output.Warn("  Skipped %s: %s",
			style.ColorBranchName(event.Branch),
			event.Message)
		return
	}

	if event.Type != get.EventCompleted {
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
func (h *SimpleGetHandler) OnRestackBranch(event actions.RestackBranchEvent) {
	h.Lock()
	defer h.Unlock()
	for _, line := range common.FormatRestackLines(event) {
		h.Output.Info("%s", line)
	}
}

// OnRestackComplete implements RestackHandler for restack phase
func (h *SimpleGetHandler) OnRestackComplete(summary actions.RestackSummary) {
	h.Lock()
	defer h.Unlock()

	if !common.RestackChanged(summary) {
		return // No restack summary needed if nothing happened
	}
	h.Output.Info("  %s", common.FormatRestackSummary(summary, 0))
	for _, conflict := range summary.Conflicts {
		h.Output.Info("  %s", common.FormatRestackResolveHint(conflict))
	}
}
