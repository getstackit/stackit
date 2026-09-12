package stack

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	stdsync "sync"

	"github.com/getstackit/stackit/internal/actions"
	syncAction "github.com/getstackit/stackit/internal/actions/sync"
	"github.com/getstackit/stackit/internal/cli/common"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/handlers"
	"github.com/getstackit/stackit/internal/output"
	"github.com/getstackit/stackit/internal/tui"
	syncComponent "github.com/getstackit/stackit/internal/tui/components/sync"
	"github.com/getstackit/stackit/internal/tui/style"
	"github.com/getstackit/stackit/internal/utils"
)

// SyncUIOptions controls interactive detail without changing action behavior.
type SyncUIOptions struct {
	// Verbose shows full branch names and revision hashes in interactive rows.
	Verbose bool
	// BranchNames seeds the short-name collision check with every branch the
	// run may render, so a row never prints a short name another branch shares.
	BranchNames []string
}

// NewSyncUI creates a runner and handler pair for sync operations.
// The runner manages terminal state; the handler processes events.
// Caller must defer runner.Cleanup() to restore terminal on exit.
func NewSyncUI(out output.Output, logger output.Logger, options ...SyncUIOptions) (*tui.Runner, syncAction.Handler) {
	if tui.IsTTY() {
		model := syncComponent.NewModel()
		runner := tui.NewRunner(model, out, logger)
		runner.Start()
		handler := NewInteractiveSyncHandler(runner, model, out, logger)
		if len(options) > 0 {
			handler.verbose = options[0].Verbose
			handler.names.Observe(options[0].BranchNames...)
		}
		return runner, handler
	}
	return nil, NewSimpleSyncHandler(out)
}

// SimpleSyncHandler provides streaming text output for non-TTY environments.
//
// Phase headers are printed lazily: a header is emitted the first time its phase
// produces an actual line, so phases that do nothing (e.g. nothing to clean or
// restack) stay silent instead of printing an empty header. Item rows are led by
// a single-width status marker (✓ done, ⚠ skipped, → in progress) from
// internal/tui/style, so they align in one column and match the interactive TUI.
type SimpleSyncHandler struct {
	common.BaseHandler
	headers *utils.LazyHeaders[syncAction.Phase]

	// restack-only state: standalone restack suppresses already-current rows
	// and reports them as a count in the summary instead.
	restackUpToDate int
	restackPrinted  bool
}

// NewSimpleSyncHandler creates a new SimpleSyncHandler
func NewSimpleSyncHandler(out output.Output) *SimpleSyncHandler {
	return &SimpleSyncHandler{
		BaseHandler: common.NewBaseHandler(out),
		headers:     utils.NewLazyHeaders[syncAction.Phase](),
	}
}

// Start implements Handler. The streaming handler keeps no per-run state.
func (h *SimpleSyncHandler) Start() {}

// EmitEvent handles progress updates
func (h *SimpleSyncHandler) EmitEvent(event syncAction.Event) {
	h.Lock()
	defer h.Unlock()

	// Phase-start events do not print eagerly. We record that the phase began;
	// the header is emitted lazily by item() the first time the phase produces a
	// real line, so phases that do nothing stay silent.
	if event.Type == syncAction.EventStarted {
		h.headers.Start(event.Phase)
		return
	}

	if isRoutineSyncEvent(event) {
		return
	}
	h.printEventLine(event)
}

// Complete is called when sync finishes
func (h *SimpleSyncHandler) Complete(summary syncAction.Summary) {
	h.Lock()
	defer h.Unlock()

	// Separate phase activity from the final line (only if anything printed).
	if h.headers.Any() {
		h.Output.Newline()
	}

	if summary.UpToDate && !summary.Failed {
		h.Output.Info("✨ Everything is up to date!")
		return
	}

	h.printSummary(summary)
}

// ensurePhaseHeader prints a phase header the first time the phase emits a line,
// with a blank line separating it from the previous phase group. Headers appear
// only for phases the caller explicitly started (the sync flow). Standalone
// restack drives items via OnRestackBranch without starting a phase, so it keeps
// its headerless output while still gaining the ✓/→ item polish.
func (h *SimpleSyncHandler) ensurePhaseHeader(phase syncAction.Phase) {
	emit, separate := h.headers.CommitOnItem(phase)
	if !emit {
		return
	}
	if separate {
		h.Output.Newline()
	}
	h.printPhaseHeader(phase)
}

// item prints one phase line, emitting the phase header first if needed.
func (h *SimpleSyncHandler) item(phase syncAction.Phase, format string, args ...any) {
	h.ensurePhaseHeader(phase)
	h.Output.Info(format, args...)
}

// Cleanup implements Handler. No-op for non-TTY handler.
func (h *SimpleSyncHandler) Cleanup() {}

// IsInteractive implements Handler. Returns false for non-TTY handler.
func (h *SimpleSyncHandler) IsInteractive() bool { return false }

// PromptMetadataConflict implements Handler. Logs warning and returns false (keep local) in non-interactive mode.
func (h *SimpleSyncHandler) PromptMetadataConflict(diff *engine.MetadataDiff) (bool, error) {
	h.Output.Warn("Metadata conflict for %s (keeping local):",
		style.ColorBranchName(diff.Branch))
	for _, fd := range diff.Differences {
		h.Output.Warn("  %s: %v (local) vs %v (remote)", fd.Field, fd.LocalValue, fd.RemoteValue)
	}
	h.Output.Info("  Use interactive mode to accept remote changes")
	return false, nil
}

// PromptOrphanedMetadata implements Handler. Logs warning and returns false (accept deletion) in non-interactive mode.
func (h *SimpleSyncHandler) PromptOrphanedMetadata(info engine.OrphanedMetadataInfo) (bool, error) {
	h.Output.Warn("Orphaned metadata for %s (accepting deletion):",
		style.ColorBranchName(info.BranchName))
	if info.LocalMeta != nil {
		if info.LocalMeta.GetLockReason().IsLocked() {
			h.Output.Warn("  lockReason: %s", info.LocalMeta.GetLockReason())
		}
		if info.LocalMeta.GetScope() != nil {
			h.Output.Warn("  scope: %s", *info.LocalMeta.GetScope())
		}
	}
	h.Output.Info("  Use interactive mode to push local changes")
	return false, nil
}

// PromptResolveConflicts implements Handler. In non-interactive mode, skips conflicts.
func (h *SimpleSyncHandler) PromptResolveConflicts(_ []string) (bool, error) {
	return false, nil
}

// PromptBranchDeletions implements Handler. In non-interactive mode, skips unpushed branches and auto-confirms the rest.
func (h *SimpleSyncHandler) PromptBranchDeletions(branches map[string]string, unpushedBranches map[string]bool) (map[string]bool, error) {
	confirmed := make(map[string]bool)
	for name := range branches {
		confirmed[name] = !unpushedBranches[name]
	}
	return confirmed, nil
}

func (h *SimpleSyncHandler) printPhaseHeader(phase syncAction.Phase) {
	switch phase {
	case syncAction.PhaseTrunk:
		h.Output.Info("📥 Pulling from remote...")
	case syncAction.PhaseBranches:
		h.Output.Info("📥 Syncing stack branches...")
	case syncAction.PhaseGitHub:
		h.Output.Info("🔄 Fetching PR info from GitHub...")
	case syncAction.PhaseClean:
		h.Output.Info("🧹 Cleaning branches...")
	case syncAction.PhaseRestack:
		h.Output.Info("📚 Restacking branches...")
	}
}

func (h *SimpleSyncHandler) printEventLine(event syncAction.Event) {
	switch event.Phase {
	case syncAction.PhaseTrunk:
		h.printTrunkEvent(event)
	case syncAction.PhaseBranches:
		h.printBranchSyncEvent(event)
	case syncAction.PhaseGitHub:
		h.printGitHubEvent(event)
	case syncAction.PhaseClean:
		h.printCleanEvent(event)
	case syncAction.PhaseRestack:
		h.printRestackEvent(event)
	}
}

func (h *SimpleSyncHandler) printTrunkEvent(event syncAction.Event) {
	if event.Type == syncAction.EventCompleted {
		switch {
		case event.HeldBy != "":
			// A held trunk reports no new revision, the same as one that needed
			// no work. Say which one happened — the remedy is in another
			// directory the user is not looking at.
			h.item(event.Phase, "  %s Held %s back: %s", style.MarkWarning(), style.ColorBranchName(event.Branch), event.HeldBy)
		case event.NewRevision != "":
			h.item(event.Phase, "  %s %s fast-forwarded to %s",
				style.MarkSuccess(),
				style.ColorBranchName(event.Branch),
				style.ColorDim(event.NewRevision))
		}
	}
}

func (h *SimpleSyncHandler) printBranchSyncEvent(event syncAction.Event) {
	switch event.Type {
	case syncAction.EventCompleted:
		if event.NewRevision != "" {
			h.item(event.Phase, "  %s %s fast-forwarded to %s",
				style.MarkSuccess(),
				style.ColorBranchName(event.Branch),
				style.ColorDim(event.NewRevision))
		}
	case syncAction.EventSkipped:
		if event.Conflict {
			h.item(event.Phase, "  %s %s diverged from remote (skipping)",
				style.MarkWarning(),
				style.ColorBranchName(event.Branch))
		}
	}
}

func (h *SimpleSyncHandler) printGitHubEvent(event syncAction.Event) {
	switch event.Type {
	case syncAction.EventProgress:
		if event.Message != "" {
			h.item(event.Phase, "  %s %s", style.MarkProgress(), event.Message)
		}
	case syncAction.EventCompleted:
		if event.Message != "" {
			h.item(event.Phase, "  %s %s", style.MarkSuccess(), event.Message)
		}
	}
}

func (h *SimpleSyncHandler) printCleanEvent(event syncAction.Event) {
	if event.Type == syncAction.EventCompleted && event.Branch != "" {
		prInfo := ""
		if event.PRNumber != nil {
			prInfo = fmt.Sprintf(" (PR #%d)", *event.PRNumber)
		}
		h.item(event.Phase, "  %s Deleted %s%s %s",
			style.MarkSuccess(),
			style.ColorBranchName(event.Branch),
			prInfo,
			style.ColorDim(event.Message))
	}
}

func (h *SimpleSyncHandler) printRestackEvent(event syncAction.Event) {
	if event.Branch == "" {
		return
	}

	prInfo := ""
	if event.PRNumber != nil {
		prInfo = fmt.Sprintf(" (PR #%d)", *event.PRNumber)
	}

	branchStr := style.ColorBranchNameIf(event.Branch, event.IsCurrent)

	switch event.Type {
	case syncAction.EventCompleted:
		switch {
		case event.NewRevision != "":
			msg := fmt.Sprintf("  %s Restacked %s%s", style.MarkSuccess(), branchStr, prInfo)
			if event.Parent != "" {
				msg += fmt.Sprintf(" on %s", style.ColorBranchName(event.Parent))
			}
			msg += fmt.Sprintf(" → %s", style.ColorDim(event.NewRevision))
			h.item(event.Phase, "%s", msg)
			if event.RerereResolvedCount > 0 {
				h.Output.Info("%s", actions.FormatRerereResolved(event.RerereResolvedCount))
			}
		case event.HeldBy != "":
			// A hold reports the same status as "nothing to do", so say which
			// one happened — the remedy lives in another worktree.
			h.item(event.Phase, "  %s Held %s%s back: %s", style.MarkWarning(), branchStr, prInfo, event.HeldBy)
		case event.IsLocked():
			h.item(event.Phase, "  %s%s %s: %s", branchStr, prInfo, common.ReasonLocked, event.LockReason)
		case event.Frozen:
			h.item(event.Phase, "  %s%s %s", branchStr, prInfo, common.ReasonFrozen)
		default:
			h.item(event.Phase, "  %s %s%s up to date", style.MarkSuccess(), branchStr, prInfo)
		}
	case syncAction.EventSkipped:
		if event.Conflict {
			h.item(event.Phase, "  %s Skipped %s%s (conflict)", style.MarkWarning(), branchStr, prInfo)
		} else {
			h.item(event.Phase, "  Skipped %s%s %s", branchStr, prInfo, style.ColorDim(event.Message))
		}
	}
}

func (h *SimpleSyncHandler) printSummary(summary syncAction.Summary) {
	if line := formatSyncSummary(summary); line != "" {
		h.Output.Info("%s", line)
	}
}

// OnRestackStart implements RestackHandler for standalone restack operations
func (h *SimpleSyncHandler) OnRestackStart(_ int) {
	// For sync, we use EmitEvent with PhaseRestack instead
	// This is here for standalone restack command usage
}

// OnRestackBranch implements RestackHandler for standalone restack operations
func (h *SimpleSyncHandler) OnRestackBranch(restack handlers.RestackBranchEvent) {
	branch := restack.Branch
	result := restack.Result
	newRev := restack.NewRevision
	prNumber := restack.PRNumber
	lockReason := restack.LockReason
	frozen := restack.Frozen
	isCurrent := restack.IsCurrent
	parent := restack.Parent
	reparented := restack.Reparented
	oldParent := restack.OldParent
	newParent := restack.NewParent
	rerereResolvedCount := restack.RerereResolvedCount
	// Already-current branches are the expected default; suppress their rows
	// and fold them into the summary count so only movement and problems print.
	if isPlainUpToDate(restack) {
		h.restackUpToDate++
		return
	}
	h.restackPrinted = true

	// Log reparenting info if applicable
	if reparented {
		h.Output.Info("Reparented %s from %s to %s (parent was merged/deleted).",
			style.ColorBranchNameIf(branch, isCurrent),
			style.ColorBranchName(oldParent),
			style.ColorBranchName(newParent))
	}

	// Convert to Event and use existing printRestackEvent
	event := syncAction.Event{
		Phase:               syncAction.PhaseRestack,
		Branch:              branch,
		PRNumber:            prNumber,
		NewRevision:         newRev,
		LockReason:          lockReason,
		Frozen:              frozen,
		HeldBy:              restack.HeldBy,
		IsCurrent:           isCurrent,
		Parent:              parent,
		RerereResolvedCount: rerereResolvedCount,
	}

	switch result {
	case syncAction.RestackDone, syncAction.RestackUnneeded:
		event.Type = syncAction.EventCompleted
	case syncAction.RestackConflict:
		event.Type = syncAction.EventSkipped
		event.Conflict = true
	case syncAction.RestackBlocked:
		event.Type = syncAction.EventSkipped
		event.Message = reasonBlockedByConflict
	}

	h.printRestackEvent(event)
}

// OnRestackComplete implements RestackHandler for standalone restack operations
func (h *SimpleSyncHandler) OnRestackComplete(summary handlers.RestackSummary) {
	if h.restackPrinted {
		h.Output.Newline()
	}
	h.Output.Info("%s", common.FormatRestackOutcome(summary, h.restackUpToDate))
}

// reasonBlockedByConflict annotates branches held back because another branch
// in their stack conflicted. The stack is applied atomically, so these were
// left untouched rather than restacked onto a moved parent.
const reasonBlockedByConflict = "(blocked by conflict in stack)"

// isPlainUpToDate reports whether a restack result is a no-op with nothing
// worth showing — the branch was already current, not locked, frozen, held back
// by a worktree, or reparented. Standalone restack suppresses these rows and
// folds them into the summary count instead. A held branch reports the same
// RestackUnneeded status as one that needed no work, so it must be excluded
// here or "I protected your work" is silently counted as "already current".
func isPlainUpToDate(event handlers.RestackBranchEvent) bool {
	return event.Result == syncAction.RestackUnneeded &&
		!event.LockReason.IsLocked() &&
		!event.Frozen &&
		event.HeldBy == "" &&
		!event.Reparented
}

// isRoutineSyncEvent reports whether a sync event only confirms the expected
// default — trunk, a synced branch, or a restacked branch that was already
// current — so both sync handlers hide its row; the final summary still says
// "Everything is up to date!" when nothing moved. Holds, locks, freezes,
// conflicts, divergence, and anything that moved a ref always show, because
// those are the rows that tell the user something happened or needs action.
func isRoutineSyncEvent(event syncAction.Event) bool {
	if event.Type != syncAction.EventCompleted || event.NewRevision != "" || event.HeldBy != "" {
		return false
	}
	switch event.Phase {
	case syncAction.PhaseTrunk, syncAction.PhaseBranches:
		return true
	case syncAction.PhaseRestack:
		return event.Branch != "" && isPlainUpToDate(handlers.RestackBranchEvent{
			Result:     syncAction.RestackUnneeded,
			LockReason: event.LockReason,
			Frozen:     event.Frozen,
		})
	}
	return false
}

// InteractiveSyncHandler provides bubbletea TUI for TTY environments
type InteractiveSyncHandler struct {
	runner       tui.Sender
	model        *syncComponent.Model
	output       output.Output
	logger       output.Logger
	mu           stdsync.Mutex
	totalOps     int
	completedOps int
	currentPhase syncAction.Phase
	verbose      bool
	names        *style.BranchNameResolver

	// restack-only: count of already-current branches whose rows were
	// suppressed, reported as a summary count instead.
	restackUpToDate int

	// released is set once a conflict prompt handed the terminal to the
	// conflict workflow. The runner is gone, so a later outcome prints as
	// plain output instead of through the TUI.
	released bool
}

// NewInteractiveSyncHandler creates a new InteractiveSyncHandler
func NewInteractiveSyncHandler(runner tui.Sender, model *syncComponent.Model, out output.Output, logger output.Logger) *InteractiveSyncHandler {
	return &InteractiveSyncHandler{
		runner: runner,
		model:  model,
		output: out,
		logger: logger,
		names:  style.NewBranchNameResolver(),
	}
}

// displayName renders a branch or parent name for an interactive row: full
// with --verbose, otherwise shortened unless another branch in this run shares
// the short form. Rows that name a ref the user must act on (diverged,
// deleted) use the full name directly instead.
func (h *InteractiveSyncHandler) displayName(name string) string {
	if h.verbose || name == "" {
		h.names.Observe(name)
		return name
	}
	return h.names.Short(name)
}

// Start implements Handler. Sync has no reliable up-front item count, so the
// live line shows activity and elapsed time rather than a k/N counter.
func (h *InteractiveSyncHandler) Start() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.totalOps = 0
	h.completedOps = 0
}

// phaseMessages maps phases to their display messages
var phaseMessages = map[syncAction.Phase]string{
	syncAction.PhaseTrunk:    "📥 Pulling from remote...",
	syncAction.PhaseBranches: "📥 Syncing stack branches...",
	syncAction.PhaseGitHub:   "🔄 Fetching PR info from GitHub...",
	syncAction.PhaseClean:    "🧹 Cleaning branches...",
	syncAction.PhaseRestack:  "📚 Restacking branches...",
}

// EmitEvent handles progress updates
func (h *InteractiveSyncHandler) EmitEvent(event syncAction.Event) {
	h.logger.Debug("InteractiveSyncHandler.EmitEvent phase=%v type=%v branch=%v", event.Phase, event.Type, event.Branch)

	h.mu.Lock()
	defer h.mu.Unlock()

	if event.Type == syncAction.EventStarted && event.Total > 0 {
		h.totalOps = event.Total
		h.completedOps = 0
		h.runner.Send(syncComponent.ProgressTickMsg{Total: event.Total})
	}
	// Handle phase transitions
	if event.Type == syncAction.EventStarted && event.Phase != h.currentPhase {
		h.currentPhase = event.Phase
		h.logger.Debug("InteractiveSyncHandler.EmitEvent phase transition phase=%v", event.Phase)
		h.runner.Send(syncComponent.PhaseStartMsg{
			Phase:   syncComponent.Phase(event.Phase),
			Message: phaseMessages[event.Phase],
		})
		return
	}

	h.names.Observe(event.Branch, event.Parent)

	// Build detail message and determine status
	detail, mark := h.formatEventDetail(event)
	if detail != "" {
		h.runner.Send(syncComponent.PhaseDetailMsg{
			Phase:   syncComponent.Phase(event.Phase),
			Message: detail,
			Mark:    mark,
		})
	}
	if event.Phase == syncAction.PhaseRestack && h.totalOps > 0 && (event.Type == syncAction.EventCompleted || event.Type == syncAction.EventSkipped) {
		h.completedOps++
		h.runner.Send(syncComponent.ProgressTickMsg{Completed: h.completedOps, Total: h.totalOps})
	}
}

// formatEventDetail formats an event into a detail string and the status mark
// that should lead its row in the TUI.
func (h *InteractiveSyncHandler) formatEventDetail(event syncAction.Event) (detail string, mark syncComponent.DetailMark) {
	if isRoutineSyncEvent(event) {
		return "", syncComponent.MarkDone
	}
	// Diverged and deleted rows keep event.Branch in full: the user needs the
	// real ref to reconcile or restore it.
	name := h.displayName(event.Branch)
	switch event.Phase {
	case syncAction.PhaseTrunk:
		if event.Type == syncAction.EventCompleted {
			switch {
			case event.HeldBy != "":
				return fmt.Sprintf("Held %s back: %s", name, event.HeldBy), syncComponent.MarkWarn
			case event.NewRevision != "":
				return fmt.Sprintf("%s fast-forwarded to %s", name, event.NewRevision), syncComponent.MarkDone
			}
		}
	case syncAction.PhaseBranches:
		switch event.Type {
		case syncAction.EventCompleted:
			if event.NewRevision != "" {
				return fmt.Sprintf("%s fast-forwarded to %s", name, event.NewRevision), syncComponent.MarkDone
			}
		case syncAction.EventSkipped:
			if event.Conflict {
				return fmt.Sprintf("%s diverged from remote (skipping)", event.Branch), syncComponent.MarkWarn
			}
		}
	case syncAction.PhaseGitHub:
		switch event.Type {
		case syncAction.EventProgress:
			if event.Message != "" {
				return event.Message, syncComponent.MarkInProgress
			}
		case syncAction.EventCompleted:
			if event.Message != "" {
				return event.Message, syncComponent.MarkDone
			}
		}
	case syncAction.PhaseClean:
		if event.Type == syncAction.EventCompleted && event.Branch != "" {
			prInfo := ""
			if event.PRNumber != nil {
				prInfo = fmt.Sprintf(" (PR #%d)", *event.PRNumber)
			}
			return fmt.Sprintf("Deleted %s%s %s", event.Branch, prInfo, event.Message), syncComponent.MarkDone
		}
	case syncAction.PhaseRestack:
		if event.Branch == "" {
			return "", syncComponent.MarkDone
		}
		prInfo := ""
		if event.PRNumber != nil {
			prInfo = fmt.Sprintf(" (PR #%d)", *event.PRNumber)
		}

		displayName := style.ColorBranchNameIf(name, event.IsCurrent)

		switch event.Type {
		case syncAction.EventCompleted:
			if event.HeldBy != "" {
				return fmt.Sprintf("Held %s%s back: %s", displayName, prInfo, event.HeldBy), syncComponent.MarkWarn
			}
			if event.NewRevision != "" {
				msg := fmt.Sprintf("Restacked %s%s", displayName, prInfo)
				if event.Parent != "" {
					msg += fmt.Sprintf(" on %s", h.displayName(event.Parent))
				}
				if h.verbose {
					msg += fmt.Sprintf(" → %s", event.NewRevision)
				}
				return msg, syncComponent.MarkDone
			}
			// Plain up-to-date rows were filtered by isRoutineSyncEvent above.
			reason := common.ReasonFrozen
			if event.IsLocked() {
				reason = fmt.Sprintf("%s: %s", common.ReasonLocked, event.LockReason)
			}
			return fmt.Sprintf("%s%s %s", displayName, prInfo, reason), syncComponent.MarkDone
		case syncAction.EventSkipped:
			if event.Conflict {
				return fmt.Sprintf("Skipped %s%s (conflict)", displayName, prInfo), syncComponent.MarkWarn
			}
			return fmt.Sprintf("Skipped %s%s %s", displayName, prInfo, event.Message), syncComponent.MarkWarn
		}
	}
	return "", syncComponent.MarkDone
}

// Complete is called when sync finishes
func (h *InteractiveSyncHandler) Complete(summary syncAction.Summary) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.finish(h.formatSummary(summary))
}

// finish delivers the final outcome line: through the TUI while it runs, or
// as plain output once a conflict prompt released the terminal. Callers hold h.mu.
func (h *InteractiveSyncHandler) finish(summaryMsg string) {
	if h.released {
		if summaryMsg != "" {
			h.output.Newline()
			h.output.Info("%s", summaryMsg)
		}
		return
	}
	h.runner.Send(syncComponent.CompleteMsg{Summary: summaryMsg})
	h.runner.Wait()
}

// formatSummary formats the sync summary
func (h *InteractiveSyncHandler) formatSummary(summary syncAction.Summary) string {
	return formatSyncSummary(summary)
}

// formatSyncSummary renders the final sync line from the action's summary,
// which carries holds, conflicts, and skips alongside the completed work.
func formatSyncSummary(summary syncAction.Summary) string {
	incomplete := summary.BranchesSkipped > 0 || len(summary.ConflictBranches) > 0 || summary.BranchesBlocked > 0 || len(summary.SkippedStacks) > 0 || len(summary.HeldBranches) > 0
	if summary.UpToDate && !incomplete && !summary.Failed {
		return "✨ Everything is up to date!"
	}
	parts := syncAction.FormatSummaryParts(summary)
	prefix := "✅ Summary: "
	switch {
	case summary.Failed:
		// A failure always reports, even when it stopped before any work.
		prefix = "✗ Sync failed: "
	case incomplete:
		prefix = "⚠ Sync incomplete: "
	case len(parts) == 0:
		// Nothing happened worth summarizing.
		return ""
	}
	return common.WithConflictAdvice(strings.TrimSuffix(prefix+strings.Join(parts, ", "), ": "), summary.ConflictBranches)
}

// OnRestackActivity reports checks before the engine applies validated results.
// It is called concurrently from validation goroutines and deliberately does
// not take h.mu: it reads no handler state, and runner.Send is safe for
// concurrent use. Taking the lock would serialize validation behind EmitEvent.
func (h *InteractiveSyncHandler) OnRestackActivity(event engine.RebaseProgress) {
	h.runner.Send(syncComponent.ActivityMsg{
		Branch:   event.Branch,
		Parent:   event.Parent,
		Finished: event.Finished,
	})
}

// OnRestackStart implements RestackHandler for standalone restack operations
func (h *InteractiveSyncHandler) OnRestackStart(branchCount int) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.totalOps = branchCount
	h.completedOps = 0

	// Update model with total ops and start restack phase
	h.runner.Send(syncComponent.ProgressTickMsg{Completed: 0, Total: branchCount})
	h.runner.Send(syncComponent.PhaseStartMsg{
		Phase:   syncComponent.Phase(syncAction.PhaseRestack),
		Message: phaseMessages[syncAction.PhaseRestack],
	})
}

// OnRestackBranch implements RestackHandler for standalone restack operations
func (h *InteractiveSyncHandler) OnRestackBranch(restack handlers.RestackBranchEvent) {
	reparented := restack.Reparented
	oldParent := restack.OldParent
	newParent := restack.NewParent
	h.mu.Lock()
	defer h.mu.Unlock()
	h.names.Observe(restack.Branch, restack.Parent, oldParent, newParent)

	// Already-current branches are the expected default; skip their rows but
	// still advance the k/N counter and count them for the summary.
	if isPlainUpToDate(restack) {
		h.restackUpToDate++
		h.completedOps++
		h.runner.Send(syncComponent.ProgressTickMsg{Completed: h.completedOps, Total: h.totalOps})
		return
	}

	// Build detail message
	detail, mark := h.formatRestackDetail(restack)
	if detail != "" {
		if reparented {
			detail = fmt.Sprintf("Reparented %s → %s. %s", h.displayName(oldParent), h.displayName(newParent), detail)
		}
		h.runner.Send(syncComponent.PhaseDetailMsg{
			Phase:   syncComponent.Phase(syncAction.PhaseRestack),
			Message: detail,
			Mark:    mark,
		})
	}

	// Update progress
	h.completedOps++
	h.runner.Send(syncComponent.ProgressTickMsg{
		Completed: h.completedOps,
		Total:     h.totalOps,
	})
}

// formatRestackDetail formats a restack event into a detail string and the
// status mark that should lead its row. The conflict case returns MarkWarn
// rather than baking a glyph into the string, so the model owns the marker (the
// streaming handler does the same).
func (h *InteractiveSyncHandler) formatRestackDetail(event handlers.RestackBranchEvent) (string, syncComponent.DetailMark) {
	prInfo := ""
	if event.PRNumber != nil {
		prInfo = fmt.Sprintf(" (PR #%d)", *event.PRNumber)
	}

	displayName := style.ColorBranchNameIf(h.displayName(event.Branch), event.IsCurrent)

	switch event.Result {
	case syncAction.RestackDone:
		msg := fmt.Sprintf("Restacked %s%s", displayName, prInfo)
		if event.Parent != "" {
			msg += fmt.Sprintf(" on %s", h.displayName(event.Parent))
		}
		if h.verbose {
			msg += fmt.Sprintf(" → %s", event.NewRevision)
		}
		if event.RerereResolvedCount > 0 {
			msg += " " + actions.FormatRerereResolved(event.RerereResolvedCount)
		}
		return msg, syncComponent.MarkDone
	case syncAction.RestackUnneeded:
		// A hold reports the same status as "nothing to do", so mark it as a
		// warning and say why — the remedy lives in another worktree.
		if event.HeldBy != "" {
			return fmt.Sprintf("Held %s%s back: %s", displayName, prInfo, event.HeldBy), syncComponent.MarkWarn
		}

		reason := common.ReasonNoRestackNeeded
		if event.LockReason.IsLocked() {
			reason = fmt.Sprintf("%s: %s", common.ReasonLocked, event.LockReason)
		} else if event.Frozen {
			reason = common.ReasonFrozen
		}

		if reason == common.ReasonNoRestackNeeded {
			return fmt.Sprintf("%s%s up to date", displayName, prInfo), syncComponent.MarkDone
		}
		return fmt.Sprintf("%s%s %s", displayName, prInfo, reason), syncComponent.MarkDone
	case syncAction.RestackConflict:
		return fmt.Sprintf("Skipped %s%s (conflict)", displayName, prInfo), syncComponent.MarkWarn
	case syncAction.RestackBlocked:
		return fmt.Sprintf("Skipped %s%s %s", displayName, prInfo, reasonBlockedByConflict), syncComponent.MarkWarn
	}
	return "", syncComponent.MarkDone
}

// OnRestackComplete implements RestackHandler for standalone restack operations
func (h *InteractiveSyncHandler) OnRestackComplete(summary handlers.RestackSummary) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.finish(common.FormatRestackOutcome(summary, h.restackUpToDate))
}

// Cleanup is a no-op - terminal cleanup is handled by the runner via defer.
func (h *InteractiveSyncHandler) Cleanup() {}

// IsInteractive implements Handler. Returns true for TTY handler.
func (h *InteractiveSyncHandler) IsInteractive() bool { return true }

// Pause releases the terminal so external prompts can run without contending
// with the active Bubble Tea program. Implements rerere.Pauser.
func (h *InteractiveSyncHandler) Pause() { h.runner.Pause() }

// Resume restores the TUI after Pause. Implements rerere.Pauser.
func (h *InteractiveSyncHandler) Resume() { h.runner.Resume() }

// describeMetadataConflict writes the human-readable details of a metadata
// conflict to out. Shared by the interactive handler and the golden transcript
// harness so both render identical text.
func describeMetadataConflict(out output.Output, diff *engine.MetadataDiff) {
	out.Info("\nMetadata differs for branch '%s':", style.ColorBranchName(diff.Branch))
	for _, fd := range diff.Differences {
		out.Info("  %s: %v (local) → %v (remote)", fd.Field, fd.LocalValue, fd.RemoteValue)
	}
	if diff.RemoteMeta != nil {
		if modBy := diff.RemoteMeta.GetLastModifiedBy(); modBy != nil {
			out.Info("  Last modified by: %s <%s>",
				modBy.GitName,
				modBy.GitEmail)
		}
	}
}

// PromptMetadataConflict implements Handler. Pauses TUI, displays conflict, prompts user.
func (h *InteractiveSyncHandler) PromptMetadataConflict(diff *engine.MetadataDiff) (bool, error) {
	h.runner.Pause()
	defer h.runner.Resume()

	describeMetadataConflict(h.output, diff)

	return tui.PromptConfirm("Accept remote metadata?", false)
}

// describeOrphanedMetadata writes the human-readable details of orphaned local
// metadata to out. Shared by the interactive handler and the golden transcript
// harness so both render identical text.
func describeOrphanedMetadata(out output.Output, info engine.OrphanedMetadataInfo) {
	out.Info("\nRemote metadata for '%s' was deleted, but you have local changes:",
		style.ColorBranchName(info.BranchName))
	if info.LocalMeta != nil {
		if info.LocalMeta.GetLockReason().IsLocked() {
			out.Info("  lockReason: %s", info.LocalMeta.GetLockReason())
		}
		if info.LocalMeta.GetScope() != nil {
			out.Info("  scope: %s", *info.LocalMeta.GetScope())
		}
	}
}

// PromptOrphanedMetadata implements Handler. Pauses TUI, displays info, prompts user.
func (h *InteractiveSyncHandler) PromptOrphanedMetadata(info engine.OrphanedMetadataInfo) (bool, error) {
	h.runner.Pause()
	defer h.runner.Resume()

	describeOrphanedMetadata(h.output, info)

	return tui.PromptConfirm("Push your local metadata to remote?", false)
}

// describeRestackConflicts writes the human-readable summary of restack
// conflicts to out. Shared by the interactive handler and the golden transcript
// harness so both render identical text.
func describeRestackConflicts(out output.Output, conflictBranches []string) {
	out.Newline()
	// out.Warn already prefixes "⚠️ "; don't hardcode another one here.
	out.Warn("Found conflicts in %d %s during restack. Affected stacks were left untouched; independent stacks were processed.",
		len(conflictBranches),
		map[bool]string{true: "branch", false: "branches"}[len(conflictBranches) == 1])
	// Bullets are detail lines, not warnings — use Info so they don't each
	// pick up a ⚠️ prefix.
	out.Info("Resolve each with:")
	for _, name := range conflictBranches {
		out.Info("  • %s", style.ColorCyan("st restack --branch "+name))
	}
	out.Newline()
}

// PromptResolveConflicts implements Handler. Pauses TUI, displays conflicts, prompts user.
func (h *InteractiveSyncHandler) PromptResolveConflicts(conflictBranches []string) (bool, error) {
	return h.promptResolveConflicts(conflictBranches, tui.PromptConfirm)
}

func (h *InteractiveSyncHandler) promptResolveConflicts(conflictBranches []string, prompt func(string, bool) (bool, error)) (bool, error) {
	h.runner.Pause()
	describeRestackConflicts(h.output, conflictBranches)
	resolve, err := prompt("Resolve conflicts now?", false)
	if resolve && err == nil {
		// The action now hands off to the real conflict workflow. Release the
		// terminal permanently so file lists and continue/abort advice are visible.
		h.runner.Send(syncComponent.CompleteMsg{})
		h.runner.Wait()
		h.runner.Cleanup()
		h.mu.Lock()
		h.released = true
		h.mu.Unlock()
	} else {
		h.runner.Resume()
	}
	return resolve, err
}

// buildDeletionOptions returns the alphabetically sorted branch names alongside
// the parallel multi-select option labels (branch name + reason, with an
// "unpushed changes" note) and their default pre-selection state (unpushed
// branches are not pre-selected). Shared by the interactive handler and the
// golden transcript harness so both render identical option labels.
func buildDeletionOptions(branches map[string]string, unpushedBranches map[string]bool) (names, options []string, preSelected []bool) {
	names = slices.Sorted(maps.Keys(branches))

	options = make([]string, len(names))
	preSelected = make([]bool, len(names))
	for i, name := range names {
		reason := branches[name]
		if unpushedBranches[name] {
			reason += " — has unpushed changes"
		}
		options[i] = fmt.Sprintf("%s (%s)", style.ColorBranchName(name), style.ColorDim(reason))
		preSelected[i] = !unpushedBranches[name] // Don't pre-select branches with unpushed changes
	}
	return names, options, preSelected
}

// PromptBranchDeletions implements Handler. Pauses TUI, displays planned deletions, prompts for each.
func (h *InteractiveSyncHandler) PromptBranchDeletions(branches map[string]string, unpushedBranches map[string]bool) (map[string]bool, error) {
	h.runner.Pause()
	defer h.runner.Resume()

	confirmed := make(map[string]bool)

	if len(branches) == 0 {
		return confirmed, nil
	}

	names, options, preSelected := buildDeletionOptions(branches, unpushedBranches)

	h.output.Newline()
	selected, err := tui.PromptMultiSelectWithDefaults("Select branches to delete:", options, preSelected)
	if err != nil {
		return confirmed, err
	}

	// Map selected options back to branch names
	selectedSet := make(map[string]bool)
	for _, opt := range selected {
		selectedSet[opt] = true
	}

	for i, name := range names {
		confirmed[name] = selectedSet[options[i]]
	}

	return confirmed, nil
}
