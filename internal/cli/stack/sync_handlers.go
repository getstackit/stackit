package stack

import (
	"fmt"
	"sort"
	"strings"
	stdsync "sync"

	syncAction "github.com/getstackit/stackit/internal/actions/sync"
	"github.com/getstackit/stackit/internal/cli/common"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/handlers"
	"github.com/getstackit/stackit/internal/output"
	"github.com/getstackit/stackit/internal/tui"
	syncComponent "github.com/getstackit/stackit/internal/tui/components/sync"
	"github.com/getstackit/stackit/internal/tui/style"
)

// NewSyncUI creates a runner and handler pair for sync operations.
// The runner manages terminal state; the handler processes events.
// Caller must defer runner.Cleanup() to restore terminal on exit.
func NewSyncUI(out output.Output, logger output.Logger) (*tui.Runner, syncAction.Handler) {
	if tui.IsTTY() {
		model := syncComponent.NewModel()
		runner := tui.NewRunner(model, out, logger)
		runner.Start()
		return runner, NewInteractiveSyncHandler(runner, model, out, logger)
	}
	return nil, NewSimpleSyncHandler(out)
}

// SimpleSyncHandler writes sync's report as plain streaming text (non-TTY).
//
// Output is emitted a phase at a time rather than a row at a time: a phase's
// header states its outcome, which is only known once the phase is over. See
// sync_report.go for what is considered worth reporting.
type SimpleSyncHandler struct {
	common.BaseHandler
	report  syncReport
	printed bool
}

// NewSimpleSyncHandler creates a new SimpleSyncHandler
func NewSimpleSyncHandler(out output.Output) *SimpleSyncHandler {
	return &SimpleSyncHandler{BaseHandler: common.NewBaseHandler(out)}
}

// Start is called at the beginning of sync. Nothing to seed: what gets reported
// is decided per phase from what that phase actually did.
func (h *SimpleSyncHandler) Start(int) {}

// EmitEvent folds an event into the report, flushing the previous phase when a
// new one begins.
func (h *SimpleSyncHandler) EmitEvent(event syncAction.Event) {
	h.Lock()
	defer h.Unlock()

	// Phase starts say nothing about outcomes; only events carry results.
	if event.Type == syncAction.EventStarted {
		return
	}
	h.printBlocks(h.report.Add(event))
}

// Complete flushes the final phase and prints the summary.
func (h *SimpleSyncHandler) Complete(summary syncAction.Summary) {
	h.Lock()
	defer h.Unlock()

	h.printBlocks(h.report.Flush())

	if summary.UpToDate {
		h.blankLine()
		h.Output.Info("%s", style.ColorDim("Already up to date."))
		return
	}

	h.blankLine()
	h.printSummary(summary)
}

// printBlocks writes finished phases: each phase's outcome header, then its rows.
func (h *SimpleSyncHandler) printBlocks(blocks []phaseBlock) {
	for _, block := range blocks {
		h.blankLine()
		if block.Header != "" {
			h.Output.Info("%s", block.Header)
		}
		for _, row := range block.Rows {
			h.Output.Info("  %s %s", row.Mark.glyph(), row.Text)
		}
		h.printed = true
	}
}

// blankLine separates blocks, but never leads the output with one.
func (h *SimpleSyncHandler) blankLine() {
	if h.printed {
		h.Output.Newline()
	}
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

func (h *SimpleSyncHandler) printSummary(summary syncAction.Summary) {
	parts := syncAction.FormatSummaryParts(summary)
	if len(parts) > 0 {
		h.Output.Info("%s", style.Bold(capitalize(strings.Join(parts, ", "))))
	}
	h.printNextSteps(summary.ConflictBranches)
}

// printNextSteps spells out the command for each conflict. The full branch name
// is used deliberately — this line is meant to be copy-pasted.
func (h *SimpleSyncHandler) printNextSteps(conflicts []string) {
	for _, conflict := range conflicts {
		h.Output.Info("   %s", style.ColorCyan(fmt.Sprintf("st restack %s", conflict)))
	}
}

// OnRestackStart implements RestackHandler for standalone restack operations.
// Sync drives the restack phase through EmitEvent instead.
func (h *SimpleSyncHandler) OnRestackStart(int) {}

// OnRestackBranch implements RestackHandler for standalone restack operations. It
// converts to an Event so standalone restack and sync report identically.
func (h *SimpleSyncHandler) OnRestackBranch(restack handlers.RestackBranchEvent) {
	h.Lock()
	defer h.Unlock()
	h.printBlocks(h.report.Add(restackEvent(restack)))
}

// OnRestackComplete implements RestackHandler for standalone restack operations.
func (h *SimpleSyncHandler) OnRestackComplete(summary handlers.RestackSummary) {
	h.Lock()
	defer h.Unlock()

	h.printBlocks(h.report.Flush())

	if summary.Restacked == 0 && summary.Skipped == 0 && len(summary.Blocked) == 0 {
		h.blankLine()
		h.Output.Info("%s", style.ColorDim("Already up to date."))
		return
	}

	h.blankLine()
	if line := formatRestackSummaryLine(summary.Restacked, summary.Skipped, len(summary.Blocked)); line != "" {
		h.Output.Info("%s", style.Bold(capitalize(line)))
	}
	h.printNextSteps(summary.Conflicts)
}

// restackEvent converts the RestackHandler callback arguments into the Event the
// report consumes, so the standalone restack command and sync's restack phase
// produce the same output from the same code.
func restackEvent(restack handlers.RestackBranchEvent) syncAction.Event {
	event := syncAction.Event{
		Phase:               syncAction.PhaseRestack,
		Branch:              restack.Branch,
		PRNumber:            restack.PRNumber,
		NewRevision:         restack.NewRevision,
		LockReason:          restack.LockReason,
		Frozen:              restack.Frozen,
		HeldBy:              restack.HeldBy,
		IsCurrent:           restack.IsCurrent,
		Parent:              restack.Parent,
		Reparented:          restack.Reparented,
		OldParent:           restack.OldParent,
		NewParent:           restack.NewParent,
		RerereResolvedCount: restack.RerereResolvedCount,
	}

	switch restack.Result {
	case syncAction.RestackDone, syncAction.RestackUnneeded:
		event.Type = syncAction.EventCompleted
	case syncAction.RestackConflict:
		event.Type = syncAction.EventSkipped
		event.Conflict = true
	case syncAction.RestackBlocked:
		event.Type = syncAction.EventSkipped
		event.Message = reasonBlockedByConflict
	}
	return event
}

// reasonBlockedByConflict annotates branches held back because another branch
// in their stack conflicted. The stack is applied atomically, so these were
// left untouched rather than restacked onto a moved parent.
const reasonBlockedByConflict = "(blocked by conflict in stack)"

// formatRestackSummaryLine renders the shared "restacked N, skipped M
// (conflict), blocked K" summary used by both restack handlers. Returns "" when
// there is nothing to summarize. Already-current branches are not counted: they
// are the expected default and reporting them tells the reader nothing.
func formatRestackSummaryLine(restacked, skipped, blocked int) string {
	parts := []string{}
	if restacked > 0 {
		parts = append(parts, fmt.Sprintf("restacked %d", restacked))
	}
	if skipped > 0 {
		parts = append(parts, fmt.Sprintf("skipped %d (conflict)", skipped))
	}
	if blocked > 0 {
		parts = append(parts, fmt.Sprintf("blocked %d", blocked))
	}
	return strings.Join(parts, ", ")
}

// InteractiveSyncHandler drives the Bubble Tea TUI. It builds the same report as
// the streaming handler and hands each finished phase to the model as composed
// lines, so both surfaces say exactly the same thing.
type InteractiveSyncHandler struct {
	runner tui.Sender
	model  *syncComponent.Model
	output output.Output
	logger output.Logger
	mu     stdsync.Mutex

	report       syncReport
	currentPhase syncAction.Phase
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

// Start is called at the beginning of sync. Nothing to seed — see
// SimpleSyncHandler.Start.
func (h *InteractiveSyncHandler) Start(int) {}

// EmitEvent folds an event into the report, printing the previous phase when a
// new one begins and moving the live line to the new phase.
func (h *InteractiveSyncHandler) EmitEvent(event syncAction.Event) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if event.Type == syncAction.EventStarted {
		if event.Phase != h.currentPhase {
			h.currentPhase = event.Phase
			h.runner.Send(syncComponent.PhaseStartMsg{Phase: syncComponent.Phase(event.Phase)})
		}
		return
	}

	// In-flight work names itself on the live line and is never printed: it is
	// superseded by what the phase reports when it ends.
	if event.Type == syncAction.EventProgress && event.Branch != "" {
		h.runner.Send(syncComponent.ActivityMsg{
			Text: "Updating PR for " + style.DisplayBranchName(event.Branch),
		})
		return
	}

	h.printBlocks(h.report.Add(event))
}

// printBlocks hands finished phases to the model as composed lines.
func (h *InteractiveSyncHandler) printBlocks(blocks []phaseBlock) {
	var lines []string
	for _, block := range blocks {
		lines = append(lines, "")
		if block.Header != "" {
			lines = append(lines, block.Header)
		}
		for _, row := range block.Rows {
			lines = append(lines, fmt.Sprintf("  %s %s", row.Mark.glyph(), row.Text))
		}
	}
	if len(lines) == 0 {
		return
	}
	h.runner.Send(syncComponent.PrintMsg{Lines: lines})
}

// Complete flushes the final phase and sends the summary.
func (h *InteractiveSyncHandler) Complete(summary syncAction.Summary) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.printBlocks(h.report.Flush())
	h.runner.Send(syncComponent.CompleteMsg{Summary: h.formatSummary(summary)})
	h.runner.Wait()
}

// formatSummary formats the sync summary
func (h *InteractiveSyncHandler) formatSummary(summary syncAction.Summary) string {
	if summary.UpToDate {
		return style.ColorDim("Already up to date.")
	}

	var lines []string
	if parts := syncAction.FormatSummaryParts(summary); len(parts) > 0 {
		lines = append(lines, style.Bold(capitalize(strings.Join(parts, ", "))))
	}
	for _, conflict := range summary.ConflictBranches {
		lines = append(lines, "   "+fmt.Sprintf("st restack %s", conflict))
	}
	return strings.Join(lines, "\n")
}

// OnRestackStart implements RestackHandler for standalone restack operations
func (h *InteractiveSyncHandler) OnRestackStart(int) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.runner.Send(syncComponent.PhaseStartMsg{Phase: syncComponent.Phase(syncAction.PhaseRestack)})
}

// OnRestackBranch implements RestackHandler for standalone restack operations
func (h *InteractiveSyncHandler) OnRestackBranch(restack handlers.RestackBranchEvent) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.printBlocks(h.report.Add(restackEvent(restack)))
}

// OnRestackComplete implements RestackHandler for standalone restack operations
func (h *InteractiveSyncHandler) OnRestackComplete(summary handlers.RestackSummary) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.printBlocks(h.report.Flush())
	h.runner.Send(syncComponent.CompleteMsg{Summary: h.formatRestackSummary(summary.Restacked, summary.Skipped, summary.Conflicts, summary.Blocked)})
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
	// The restack block already names each conflicted branch, and the summary
	// already prints the command to resolve it. This only has to state the
	// stakes before asking, so it stays to one line.
	out.Newline()
	out.Warn("%s conflicted; the rest of the stack was restacked.",
		countBranches(len(conflictBranches)))
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
	names = make([]string, 0, len(branches))
	for name := range branches {
		names = append(names, name)
	}
	sort.Strings(names)

	options = make([]string, len(names))
	preSelected = make([]bool, len(names))
	for i, name := range names {
		reason := branches[name]
		if unpushedBranches[name] {
			reason += " — has unpushed changes"
		}
		options[i] = fmt.Sprintf("%s (%s)", style.ColorBranchName(style.DisplayBranchName(name)), style.ColorDim(reason))
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
func (h *InteractiveSyncHandler) formatRestackSummary(restacked, skipped int, conflicts, blocked []string) string {
	if restacked == 0 && skipped == 0 && len(blocked) == 0 {
		return style.ColorDim("Already up to date.")
	}

	var lines []string
	if summary := formatRestackSummaryLine(restacked, skipped, len(blocked)); summary != "" {
		lines = append(lines, style.Bold(capitalize(summary)))
	}
	for _, conflict := range conflicts {
		lines = append(lines, "   "+fmt.Sprintf("st restack %s", conflict))
	}
	return strings.Join(lines, "\n")
}

// capitalize upper-cases the first letter, so a summary assembled from lowercase
// fragments reads as a sentence when it stands on its own line.
func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
