package stack

import (
	"fmt"
	"sort"
	"strings"
	stdsync "sync"

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
	if tui.IsTTY() {
		model := syncComponent.NewModel(0) // Start with 0, will be updated in Start()
		runner := tui.NewRunner(model, out, logger)
		runner.Start()
		return runner, NewInteractiveSyncHandler(runner, model, out, logger)
	}
	return nil, NewSimpleSyncHandler(out)
}

// SimpleSyncHandler provides streaming text output for non-TTY environments
type SimpleSyncHandler struct {
	common.BaseHandler
	currentPhase syncAction.Phase
	totalOps     int
	currentOp    int
}

// NewSimpleSyncHandler creates a new SimpleSyncHandler
func NewSimpleSyncHandler(out output.Output) *SimpleSyncHandler {
	return &SimpleSyncHandler{
		BaseHandler: common.NewBaseHandler(out),
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

	// Handle phase transitions
	if event.Type == syncAction.EventStarted && event.Phase != h.currentPhase {
		h.currentPhase = event.Phase
		h.printPhaseHeader(event.Phase)
		return
	}

	// Handle progress events
	h.currentOp++
	h.printEventLine(event)
}

// Complete is called when sync finishes
func (h *SimpleSyncHandler) Complete(summary syncAction.Summary) {
	h.Lock()
	defer h.Unlock()

	// Print blank line before summary
	h.Output.Newline()

	// Handle up-to-date case
	if summary.UpToDate {
		h.Output.Info("✨ Everything is up to date!")
		return
	}

	// Print summary
	h.printSummary(summary)
}

// Cleanup implements Handler. No-op for non-TTY handler.
func (h *SimpleSyncHandler) Cleanup() {}

// IsInteractive implements Handler. Returns false for non-TTY handler.
func (h *SimpleSyncHandler) IsInteractive() bool { return false }

// PromptMetadataConflict implements Handler. Logs warning and returns false (keep local) in non-interactive mode.
func (h *SimpleSyncHandler) PromptMetadataConflict(diff *engine.MetadataDiff) (bool, error) {
	h.Output.Warn("Metadata conflict for %s (keeping local):",
		style.ColorBranchName(diff.Branch, false))
	for _, fd := range diff.Differences {
		h.Output.Warn("  %s: %v (local) vs %v (remote)", fd.Field, fd.LocalValue, fd.RemoteValue)
	}
	h.Output.Info("  Use interactive mode to accept remote changes")
	return false, nil
}

// PromptOrphanedMetadata implements Handler. Logs warning and returns false (accept deletion) in non-interactive mode.
func (h *SimpleSyncHandler) PromptOrphanedMetadata(info engine.OrphanedMetadataInfo) (bool, error) {
	h.Output.Warn("Orphaned metadata for %s (accepting deletion):",
		style.ColorBranchName(info.BranchName, false))
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
	// Add spacing between phases (but not before first phase)
	if h.currentPhase != "" {
		h.Output.Newline()
	}

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
		if event.NewRevision != "" {
			h.Output.Info("  %s fast-forwarded to %s",
				style.ColorBranchName(event.Branch, false),
				style.ColorDim(event.NewRevision))
		} else {
			h.Output.Info("  %s is up to date", style.ColorBranchName(event.Branch, false))
		}
	}
}

func (h *SimpleSyncHandler) printBranchSyncEvent(event syncAction.Event) {
	switch event.Type {
	case syncAction.EventCompleted:
		if event.NewRevision != "" {
			h.Output.Info("  %s fast-forwarded to %s",
				style.ColorBranchName(event.Branch, false),
				style.ColorDim(event.NewRevision))
		} else {
			h.Output.Info("  %s is up to date", style.ColorBranchName(event.Branch, false))
		}
	case syncAction.EventSkipped:
		if event.Conflict {
			h.Output.Warn("  ⚠️ %s diverged from remote (skipping)",
				style.ColorBranchName(event.Branch, false))
		}
	}
}

func (h *SimpleSyncHandler) printGitHubEvent(event syncAction.Event) {
	switch event.Type {
	case syncAction.EventProgress:
		if event.Branch != "" {
			h.Output.Info("  Updating PR for %s", style.ColorBranchName(event.Branch, false))
		}
	case syncAction.EventCompleted:
		if event.Message != "" {
			h.Output.Info("  %s", event.Message)
		}
	}
}

func (h *SimpleSyncHandler) printCleanEvent(event syncAction.Event) {
	if event.Type == syncAction.EventCompleted && event.Branch != "" {
		prInfo := ""
		if event.PRNumber != nil {
			prInfo = fmt.Sprintf(" (PR #%d)", *event.PRNumber)
		}
		h.Output.Info("  Deleted %s%s %s",
			style.ColorBranchName(event.Branch, false),
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

	switch event.Type {
	case syncAction.EventCompleted:
		if event.NewRevision != "" {
			msg := fmt.Sprintf("Restacked %s%s", style.ColorBranchName(event.Branch, event.IsCurrent), prInfo)
			if event.Parent != "" {
				msg += fmt.Sprintf(" on %s", style.ColorBranchName(event.Parent, false))
			}
			msg += fmt.Sprintf(" -> %s", style.ColorDim(event.NewRevision))
			h.Output.Info("  %s", msg)
			if event.RerereResolvedCount > 0 {
				h.Output.Info("%s", actions.FormatRerereResolved(event.RerereResolvedCount))
			}
		} else {
			reason := common.ReasonNoRestackNeeded
			if event.IsLocked() {
				reason = fmt.Sprintf("%s: %s", common.ReasonLocked, event.LockReason)
			} else if event.Frozen {
				reason = common.ReasonFrozen
			}

			msg := fmt.Sprintf("%s%s %s", style.ColorBranchName(event.Branch, event.IsCurrent), prInfo, reason)
			if reason == common.ReasonNoRestackNeeded {
				msg = fmt.Sprintf("%s%s up to date", style.ColorBranchName(event.Branch, event.IsCurrent), prInfo)
			}
			h.Output.Info("  %s", msg)
		}
	case syncAction.EventSkipped:
		if event.Conflict {
			h.Output.Warn("Skipped %s%s (conflict)",
				style.ColorBranchName(event.Branch, event.IsCurrent),
				prInfo)
		} else {
			h.Output.Info("  Skipped %s%s %s",
				style.ColorBranchName(event.Branch, event.IsCurrent),
				prInfo,
				style.ColorDim(event.Message))
		}
	}
}

func (h *SimpleSyncHandler) printSummary(summary syncAction.Summary) {
	parts := syncAction.FormatSummaryParts(summary)

	if len(parts) > 0 {
		h.Output.Info("✅ Summary: %s", strings.Join(parts, ", "))
	}

	// Print actionable advice for conflicts
	if len(summary.ConflictBranches) > 0 {
		h.Output.Info("  Run %s to resolve and continue",
			style.ColorCyan(fmt.Sprintf("st restack %s", summary.ConflictBranches[0])))
	}
}

// OnRestackStart implements RestackHandler for standalone restack operations
func (h *SimpleSyncHandler) OnRestackStart(_ int) {
	// For sync, we use EmitEvent with PhaseRestack instead
	// This is here for standalone restack command usage
}

// OnRestackBranch implements RestackHandler for standalone restack operations
func (h *SimpleSyncHandler) OnRestackBranch(branch string, result syncAction.RestackResult, newRev string, prNumber *int, lockReason engine.LockReason, frozen bool, isCurrent bool, parent string, reparented bool, oldParent, newParent string, rerereResolvedCount int) {
	// Log reparenting info if applicable
	if reparented {
		h.Output.Info("Reparented %s from %s to %s (parent was merged/deleted).",
			style.ColorBranchName(branch, isCurrent),
			style.ColorBranchName(oldParent, false),
			style.ColorBranchName(newParent, false))
	}

	// Convert to Event and use existing printRestackEvent
	event := syncAction.Event{
		Phase:               syncAction.PhaseRestack,
		Branch:              branch,
		PRNumber:            prNumber,
		NewRevision:         newRev,
		LockReason:          lockReason,
		Frozen:              frozen,
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
	}

	h.printRestackEvent(event)
}

// OnRestackComplete implements RestackHandler for standalone restack operations
func (h *SimpleSyncHandler) OnRestackComplete(restacked, skipped int, conflicts []string) {
	h.Output.Newline()

	if restacked == 0 && skipped == 0 {
		h.Output.Info("✨ All branches are up to date!")
		return
	}

	parts := []string{}
	if restacked > 0 {
		parts = append(parts, fmt.Sprintf("restacked %d", restacked))
	}
	if skipped > 0 {
		parts = append(parts, fmt.Sprintf("skipped %d (conflict)", skipped))
	}

	if len(parts) > 0 {
		h.Output.Info("✅ Summary: %s", strings.Join(parts, ", "))
	}

	if len(conflicts) > 0 {
		h.Output.Info("  Run %s to resolve and continue",
			style.ColorCyan(fmt.Sprintf("st restack %s", conflicts[0])))
	}
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
		h.logger.Debug("InteractiveSyncHandler.EmitEvent phase transition phase=%v", event.Phase)
		h.runner.Send(syncComponent.PhaseStartMsg{
			Phase:   syncComponent.Phase(event.Phase),
			Message: phaseMessages[event.Phase],
		})
		return
	}

	// Build detail message and determine status
	detail, isWarn := h.formatEventDetail(event)
	if detail != "" {
		h.runner.Send(syncComponent.PhaseDetailMsg{
			Phase:   syncComponent.Phase(event.Phase),
			Message: detail,
			IsWarn:  isWarn,
		})
	}

	// Update progress
	h.completedOps++
	h.runner.Send(syncComponent.ProgressTickMsg{
		Completed: h.completedOps,
		Total:     h.totalOps,
	})
}

// formatEventDetail formats an event into a detail string with warning status
func (h *InteractiveSyncHandler) formatEventDetail(event syncAction.Event) (detail string, isWarn bool) {
	switch event.Phase {
	case syncAction.PhaseTrunk:
		if event.Type == syncAction.EventCompleted {
			if event.NewRevision != "" {
				return fmt.Sprintf("%s fast-forwarded to %s", event.Branch, event.NewRevision), false
			}
			return fmt.Sprintf("%s is up to date", event.Branch), false
		}
	case syncAction.PhaseBranches:
		switch event.Type {
		case syncAction.EventCompleted:
			if event.NewRevision != "" {
				return fmt.Sprintf("%s fast-forwarded to %s", event.Branch, event.NewRevision), false
			}
			return fmt.Sprintf("%s is up to date", event.Branch), false
		case syncAction.EventSkipped:
			if event.Conflict {
				return fmt.Sprintf("%s diverged from remote (skipping)", event.Branch), true
			}
		}
	case syncAction.PhaseGitHub:
		switch event.Type {
		case syncAction.EventProgress:
			if event.Branch != "" {
				return fmt.Sprintf("Updating PR for %s", event.Branch), false
			}
		case syncAction.EventCompleted:
			if event.Message != "" {
				return event.Message, false
			}
		}
	case syncAction.PhaseClean:
		if event.Type == syncAction.EventCompleted && event.Branch != "" {
			prInfo := ""
			if event.PRNumber != nil {
				prInfo = fmt.Sprintf(" (PR #%d)", *event.PRNumber)
			}
			return fmt.Sprintf("Deleted %s%s %s", event.Branch, prInfo, event.Message), false
		}
	case syncAction.PhaseRestack:
		if event.Branch == "" {
			return "", false
		}
		prInfo := ""
		if event.PRNumber != nil {
			prInfo = fmt.Sprintf(" (PR #%d)", *event.PRNumber)
		}

		displayName := style.ColorBranchName(event.Branch, event.IsCurrent)

		switch event.Type {
		case syncAction.EventCompleted:
			if event.NewRevision != "" {
				msg := fmt.Sprintf("Restacked %s%s", displayName, prInfo)
				if event.Parent != "" {
					msg += fmt.Sprintf(" on %s", event.Parent)
				}
				msg += fmt.Sprintf(" -> %s", event.NewRevision)
				return msg, false
			}
			reason := common.ReasonNoRestackNeeded
			if event.IsLocked() {
				reason = fmt.Sprintf("%s: %s", common.ReasonLocked, event.LockReason)
			} else if event.Frozen {
				reason = common.ReasonFrozen
			}

			if reason == common.ReasonNoRestackNeeded {
				return fmt.Sprintf("%s%s up to date", displayName, prInfo), false
			}
			return fmt.Sprintf("%s%s %s", displayName, prInfo, reason), false
		case syncAction.EventSkipped:
			if event.Conflict {
				return fmt.Sprintf("Skipped %s%s (conflict)", displayName, prInfo), true
			}
			return fmt.Sprintf("Skipped %s%s %s", displayName, prInfo, event.Message), false
		}
	}
	return "", false
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
	if summary.UpToDate {
		return "✨ Everything is up to date!"
	}

	parts := syncAction.FormatSummaryParts(summary)

	result := ""
	if len(parts) > 0 {
		result = "✅ Summary: " + strings.Join(parts, ", ")
	}

	// Add actionable advice for conflicts
	if len(summary.ConflictBranches) > 0 {
		result += fmt.Sprintf("\n   Run 'st restack %s' to resolve and continue", summary.ConflictBranches[0])
	}

	return result
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
func (h *InteractiveSyncHandler) OnRestackBranch(branch string, result syncAction.RestackResult, newRev string, prNumber *int, lockReason engine.LockReason, frozen bool, isCurrent bool, parent string, reparented bool, oldParent, newParent string, rerereResolvedCount int) {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Build detail message
	detail := h.formatRestackDetail(branch, result, newRev, prNumber, lockReason, frozen, isCurrent, parent, rerereResolvedCount)
	if detail != "" {
		if reparented {
			detail = fmt.Sprintf("Reparented %s -> %s. %s", oldParent, newParent, detail)
		}
		h.runner.Send(syncComponent.PhaseDetailMsg{
			Phase:   syncComponent.Phase(syncAction.PhaseRestack),
			Message: detail,
		})
	}

	// Update progress
	h.completedOps++
	h.runner.Send(syncComponent.ProgressTickMsg{
		Completed: h.completedOps,
		Total:     h.totalOps,
	})
}

// formatRestackDetail formats a restack event into a detail string
func (h *InteractiveSyncHandler) formatRestackDetail(branch string, result syncAction.RestackResult, newRev string, prNumber *int, lockReason engine.LockReason, frozen bool, isCurrent bool, parent string, rerereResolvedCount int) string {
	prInfo := ""
	if prNumber != nil {
		prInfo = fmt.Sprintf(" (PR #%d)", *prNumber)
	}

	displayName := style.ColorBranchName(branch, isCurrent)

	switch result {
	case syncAction.RestackDone:
		msg := fmt.Sprintf("Restacked %s%s", displayName, prInfo)
		if parent != "" {
			msg += fmt.Sprintf(" on %s", parent)
		}
		msg += fmt.Sprintf(" -> %s", newRev)
		if rerereResolvedCount > 0 {
			msg += " " + actions.FormatRerereResolved(rerereResolvedCount)
		}
		return msg
	case syncAction.RestackUnneeded:
		reason := common.ReasonNoRestackNeeded
		if lockReason.IsLocked() {
			reason = fmt.Sprintf("%s: %s", common.ReasonLocked, lockReason)
		} else if frozen {
			reason = common.ReasonFrozen
		}

		if reason == common.ReasonNoRestackNeeded {
			return fmt.Sprintf("%s%s up to date", displayName, prInfo)
		}
		return fmt.Sprintf("%s%s %s", displayName, prInfo, reason)
	case syncAction.RestackConflict:
		return fmt.Sprintf("⚠️ Skipped %s%s (conflict)", displayName, prInfo)
	}
	return ""
}

// OnRestackComplete implements RestackHandler for standalone restack operations
func (h *InteractiveSyncHandler) OnRestackComplete(restacked, skipped int, conflicts []string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Build summary message
	summaryMsg := h.formatRestackSummary(restacked, skipped, conflicts)

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
	out.Info("\nMetadata differs for branch '%s':", style.ColorBranchName(diff.Branch, false))
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
		style.ColorBranchName(info.BranchName, false))
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
	out.Warn("⚠️  Found conflicts in %d %s during restack:",
		len(conflictBranches),
		map[bool]string{true: "branch", false: "branches"}[len(conflictBranches) == 1])
	for _, name := range conflictBranches {
		out.Warn("  • %s", style.ColorBranchName(name, false))
	}
	out.Newline()
	out.Info("Branches that could be restacked cleanly have been restacked.")
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
		options[i] = fmt.Sprintf("%s (%s)", style.ColorBranchName(name, false), style.ColorDim(reason))
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
func (h *InteractiveSyncHandler) formatRestackSummary(restacked, skipped int, conflicts []string) string {
	if restacked == 0 && skipped == 0 {
		return "✨ All branches are up to date!"
	}

	parts := []string{}
	if restacked > 0 {
		parts = append(parts, fmt.Sprintf("restacked %d", restacked))
	}
	if skipped > 0 {
		parts = append(parts, fmt.Sprintf("skipped %d (conflict)", skipped))
	}

	result := ""
	if len(parts) > 0 {
		result = "✅ Summary: " + strings.Join(parts, ", ")
	}

	if len(conflicts) > 0 {
		result += fmt.Sprintf("\n   Run 'st restack %s' to resolve and continue", conflicts[0])
	}

	return result
}
