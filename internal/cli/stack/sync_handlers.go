package stack

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	stdsync "sync"

	"github.com/getstackit/stackit/internal/term"

	"github.com/getstackit/stackit/internal/actions"
	syncAction "github.com/getstackit/stackit/internal/actions/sync"
	"github.com/getstackit/stackit/internal/cli/common"
	"github.com/getstackit/stackit/internal/engine"

	"github.com/getstackit/stackit/internal/output"
	"github.com/getstackit/stackit/internal/tui"
	syncComponent "github.com/getstackit/stackit/internal/tui/components/sync"
	"github.com/getstackit/stackit/internal/tui/style"
)

// NewSyncUI creates a runner and handler pair for sync operations.
// The runner manages terminal state; the handler processes events.
// Caller must defer runner.Cleanup() to restore terminal on exit.
func NewSyncUI(out output.Output, logger output.Logger) (*tui.Runner, syncAction.Handler) {
	if term.IsInteractive() {
		model := syncComponent.NewModel(0) // Start with 0, will be updated in Start()
		runner := tui.NewRunner(model, out, logger)
		runner.Start()
		return runner, NewInteractiveSyncHandler(runner, model, out, logger)
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
	totalOps  int
	currentOp int
	headers   *syncComponent.LazyHeaders[syncAction.Phase]

	// restack-only state: standalone restack suppresses already-current rows
	// and reports them as a count in the summary instead.
	restackUpToDate int
	restackPrinted  bool

	// inSyncRestack is set once sync opens its restack phase. Sync lists
	// every branch it restacks, so already-current rows are not suppressed.
	inSyncRestack bool
}

// NewSimpleSyncHandler creates a new SimpleSyncHandler
func NewSimpleSyncHandler(out output.Output) *SimpleSyncHandler {
	return &SimpleSyncHandler{
		BaseHandler: common.NewBaseHandler(out),
		headers:     syncComponent.NewLazyHeaders[syncAction.Phase](),
	}
}

// Start is called at the beginning of sync
func (h *SimpleSyncHandler) Start(totalOps int) {
	h.Lock()
	defer h.Unlock()
	h.totalOps = totalOps
	h.currentOp = 0
}

// EmitEvent handles progress updates
func (h *SimpleSyncHandler) EmitEvent(event syncAction.Event) {
	h.Lock()
	defer h.Unlock()

	// Phase-start events do not print eagerly. We record that the phase began;
	// the header is emitted lazily by item() the first time the phase produces a
	// real line, so phases that do nothing stay silent.
	if event.Type == syncAction.EventStarted {
		h.headers.Start(event.Phase)
		if event.Phase == syncAction.PhaseRestack {
			h.inSyncRestack = true
		}
		return
	}

	h.currentOp++
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

	if line := formatSyncSummary(summary, "  "); line != "" {
		h.Output.Info("%s", line)
	}
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
		default:
			h.item(event.Phase, "  %s %s is up to date", style.MarkSuccess(), style.ColorBranchName(event.Branch))
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
		} else {
			h.item(event.Phase, "  %s %s is up to date", style.MarkSuccess(), style.ColorBranchName(event.Branch))
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
		if event.Branch != "" {
			h.item(event.Phase, "  %s Updating PR for %s", style.MarkProgress(), style.ColorBranchName(event.Branch))
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

// OnRestackStart implements RestackHandler for standalone restack operations
func (h *SimpleSyncHandler) OnRestackStart(_ int) {
	// For sync, we use EmitEvent with PhaseRestack instead
	// This is here for standalone restack command usage
}

// OnRestackBranch implements RestackHandler for sync's restack phase and
// standalone restack.
func (h *SimpleSyncHandler) OnRestackBranch(restack actions.RestackBranchEvent) {
	// Already-current branches are the expected default for standalone
	// restack; suppress their rows and fold them into the summary count so
	// only movement and problems print.
	if !h.inSyncRestack && restack.IsPlainUpToDate() {
		h.restackUpToDate++
		return
	}
	h.restackPrinted = true
	for _, line := range common.FormatRestackLines(restack) {
		h.item(syncAction.PhaseRestack, "%s", line)
	}
}

// OnRestackComplete implements RestackHandler for standalone restack operations
func (h *SimpleSyncHandler) OnRestackComplete(summary actions.RestackSummary) {
	// Only separate from prior rows when some actually printed; a pure no-op
	// prints just the one-line summary with no leading blank.
	if h.restackPrinted {
		h.Output.Newline()
	}

	h.Output.Info("%s", common.FormatRestackOutcome(summary, h.restackUpToDate, "  "))
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

	// restack-only: count of already-current branches whose rows were
	// suppressed, reported as a summary count instead.
	restackUpToDate int

	// inSyncRestack is set once sync opens its restack phase; see
	// SimpleSyncHandler.inSyncRestack.
	inSyncRestack bool
}

// NewInteractiveSyncHandler creates a new InteractiveSyncHandler
func NewInteractiveSyncHandler(runner tui.Sender, model *syncComponent.Model, out output.Output, logger output.Logger) *InteractiveSyncHandler {
	return &InteractiveSyncHandler{
		runner: runner,
		model:  model,
		output: out,
		logger: logger,
	}
}

// Start is called at the beginning of sync
func (h *InteractiveSyncHandler) Start(totalOps int) {
	h.logger.Debug("InteractiveSyncHandler.Start entering totalOps=%v", totalOps)

	h.mu.Lock()
	defer h.mu.Unlock()

	h.totalOps = totalOps
	h.completedOps = 0

	// Update model with total ops
	h.runner.Send(syncComponent.ProgressTickMsg{Completed: 0, Total: totalOps})

	h.logger.Debug("InteractiveSyncHandler.Start completed")
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

	// Handle phase transitions
	if event.Type == syncAction.EventStarted && event.Phase != h.currentPhase {
		h.currentPhase = event.Phase
		if event.Phase == syncAction.PhaseRestack {
			h.inSyncRestack = true
		}
		h.logger.Debug("InteractiveSyncHandler.EmitEvent phase transition phase=%v", event.Phase)
		h.runner.Send(syncComponent.PhaseStartMsg{
			Phase:   syncComponent.Phase(event.Phase),
			Message: phaseMessages[event.Phase],
		})
		return
	}

	// Build detail message and determine status
	detail, mark := h.formatEventDetail(event)
	if detail != "" {
		h.runner.Send(syncComponent.PhaseDetailMsg{
			Phase:   syncComponent.Phase(event.Phase),
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

// formatEventDetail formats an event into a detail string and the status mark
// that should lead its row in the TUI.
func (h *InteractiveSyncHandler) formatEventDetail(event syncAction.Event) (detail string, mark syncComponent.DetailMark) {
	switch event.Phase {
	case syncAction.PhaseTrunk:
		if event.Type == syncAction.EventCompleted {
			switch {
			case event.HeldBy != "":
				return fmt.Sprintf("Held %s back: %s", event.Branch, event.HeldBy), syncComponent.MarkWarn
			case event.NewRevision != "":
				return fmt.Sprintf("%s fast-forwarded to %s", event.Branch, event.NewRevision), syncComponent.MarkDone
			}
			return fmt.Sprintf("%s is up to date", event.Branch), syncComponent.MarkDone
		}
	case syncAction.PhaseBranches:
		switch event.Type {
		case syncAction.EventCompleted:
			if event.NewRevision != "" {
				return fmt.Sprintf("%s fast-forwarded to %s", event.Branch, event.NewRevision), syncComponent.MarkDone
			}
			return fmt.Sprintf("%s is up to date", event.Branch), syncComponent.MarkDone
		case syncAction.EventSkipped:
			if event.Conflict {
				return fmt.Sprintf("%s diverged from remote (skipping)", event.Branch), syncComponent.MarkWarn
			}
		}
	case syncAction.PhaseGitHub:
		switch event.Type {
		case syncAction.EventProgress:
			if event.Branch != "" {
				return fmt.Sprintf("Updating PR for %s", event.Branch), syncComponent.MarkInProgress
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
	}
	return "", syncComponent.MarkDone
}

// Complete is called when sync finishes
func (h *InteractiveSyncHandler) Complete(summary syncAction.Summary) {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Build summary message
	summaryMsg := h.formatSummary(summary)

	// Send complete message
	h.runner.Send(syncComponent.CompleteMsg{Summary: summaryMsg})
	h.runner.Wait()
}

// formatSummary formats the sync summary
func (h *InteractiveSyncHandler) formatSummary(summary syncAction.Summary) string {
	return formatSyncSummary(summary, "   ")
}

// formatSyncSummary renders the final sync line from the action's summary,
// which carries holds, conflicts, and skips alongside the completed work.
// Anything left outstanding makes it read as incomplete, never as success.
func formatSyncSummary(summary syncAction.Summary, indent string) string {
	incomplete := summary.Incomplete()
	if summary.UpToDate && !incomplete {
		return "✨ Everything is up to date!"
	}
	parts := formatSyncSummaryParts(summary)
	if len(parts) == 0 && !incomplete {
		// Nothing happened worth summarizing (e.g. sync stopped on an error).
		return ""
	}
	prefix := "✅ Summary: "
	if incomplete {
		prefix = "⚠ Sync incomplete: "
	}
	return common.WithResolveHints(prefix+strings.Join(parts, ", "), summary.ConflictBranches, indent)
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
func (h *InteractiveSyncHandler) OnRestackBranch(restack actions.RestackBranchEvent) {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Already-current branches are the expected default; skip their rows but
	// still advance the progress bar and count them for the summary.
	if !h.inSyncRestack && restack.IsPlainUpToDate() {
		h.restackUpToDate++
		h.completedOps++
		h.runner.Send(syncComponent.ProgressTickMsg{Completed: h.completedOps, Total: h.totalOps})
		return
	}

	if reparent := common.FormatRestackReparent(restack); reparent != "" {
		h.sendRestackDetail(reparent, syncComponent.MarkDone)
	}
	row, status := common.FormatRestackRow(restack)
	mark := syncComponent.MarkDone
	if status == common.RestackRowWarning {
		mark = syncComponent.MarkWarn
	}
	h.sendRestackDetail(row, mark)

	// Update progress
	h.completedOps++
	h.runner.Send(syncComponent.ProgressTickMsg{
		Completed: h.completedOps,
		Total:     h.totalOps,
	})
}

func (h *InteractiveSyncHandler) sendRestackDetail(message string, mark syncComponent.DetailMark) {
	h.runner.Send(syncComponent.PhaseDetailMsg{
		Phase:   syncComponent.Phase(syncAction.PhaseRestack),
		Message: message,
		Mark:    mark,
	})
}

// OnRestackComplete implements RestackHandler for standalone restack operations
func (h *InteractiveSyncHandler) OnRestackComplete(summary actions.RestackSummary) {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Build summary message
	summaryMsg := h.formatRestackSummary(summary)

	// Send complete message
	h.runner.Send(syncComponent.CompleteMsg{Summary: summaryMsg})
	h.runner.Wait()
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
	h.runner.Pause()
	defer h.runner.Resume()

	describeRestackConflicts(h.output, conflictBranches)

	return tui.PromptConfirm("Resolve conflicts now?", false)
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

// formatRestackSummary formats the restack summary
func (h *InteractiveSyncHandler) formatRestackSummary(summary actions.RestackSummary) string {
	return common.FormatRestackOutcome(summary, h.restackUpToDate, "   ")
}

// formatSyncSummaryParts returns the sync summary parts, shared between
// SimpleSyncHandler and InteractiveSyncHandler.
func formatSyncSummaryParts(summary syncAction.Summary) []string {
	parts := []string{}

	// Holds lead: the remedy lives in another worktree the user is not
	// looking at, so it must not be buried behind routine counts.
	if len(summary.HeldBranches) > 0 {
		parts = append(parts, fmt.Sprintf("held %d (worktree)", len(summary.HeldBranches)))
	}
	if summary.TrunkUpdated {
		parts = append(parts, "pulled trunk")
	}
	if summary.BranchesSynced > 0 {
		parts = append(parts, fmt.Sprintf("synced %d branch%s", summary.BranchesSynced, pluralES(summary.BranchesSynced)))
	}
	if summary.BranchesRestacked > 0 {
		parts = append(parts, fmt.Sprintf("restacked %d", summary.BranchesRestacked))
	}
	if summary.BranchesDeleted > 0 {
		parts = append(parts, fmt.Sprintf("deleted %d", summary.BranchesDeleted))
	}
	if summary.WorktreesCleaned > 0 {
		parts = append(parts, fmt.Sprintf("cleaned %d worktree%s", summary.WorktreesCleaned, plural(summary.WorktreesCleaned)))
	}
	if summary.BranchesSkipped > 0 {
		parts = append(parts, fmt.Sprintf("skipped %d (conflict)", summary.BranchesSkipped))
	}
	if summary.BranchesBlocked > 0 {
		parts = append(parts, fmt.Sprintf("blocked %d", summary.BranchesBlocked))
	}
	if len(summary.SkippedStacks) > 0 {
		parts = append(parts, fmt.Sprintf("skipped %d stack%s (dirty worktree)", len(summary.SkippedStacks), plural(len(summary.SkippedStacks))))
	}

	return parts
}

// plural returns "s" if count != 1, otherwise empty string
func plural(count int) string {
	if count == 1 {
		return ""
	}
	return "s"
}

// pluralES returns "es" if count != 1, otherwise empty string (for "branch" -> "branches")
func pluralES(count int) string {
	if count == 1 {
		return ""
	}
	return "es"
}
