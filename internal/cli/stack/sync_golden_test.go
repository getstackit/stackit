package stack

import (
	"bytes"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	syncAction "github.com/getstackit/stackit/internal/actions/sync"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/internal/output"
	"github.com/getstackit/stackit/testhelpers/golden"
)

// This file is a golden-output harness for the `sync` command. A scenario is a
// declarative list of steps describing what sync's Handler is told (phase
// starts, progress events, prompts + the user's scripted answer) plus a final
// summary. Each scenario is "played" through sync's REAL renderer and captured
// to a checked-in .golden transcript under testdata/sync/, so command output is
// reviewable as a diff and regenerated with:
//
//	go test ./internal/cli/stack -run Golden -update
//
// It runs in microseconds with no git repository: events and the summary render
// through the real SimpleSyncHandler, and prompt descriptions through the real
// describe* helpers (shared with InteractiveSyncHandler). Only the input widget
// itself is rendered as a flat transcript line, since the real widget is a
// bubbletea program that can't be captured as text. This tests presentation; it
// does not re-test which events Action decides to emit (covered elsewhere).
//
// To extend this pattern to another command, add a small transcript handler +
// step constructors for that command's Handler and reuse testhelpers/golden.

// transcriptSyncHandler renders sync output to a buffer for golden tests. It
// embeds the real SimpleSyncHandler (reusing its event/summary rendering
// verbatim) and adds transcript rendering for the interactive prompt flows.
type transcriptSyncHandler struct {
	*SimpleSyncHandler
}

func newTranscriptSyncHandler(out output.Output) *transcriptSyncHandler {
	return &transcriptSyncHandler{SimpleSyncHandler: NewSimpleSyncHandler(out)}
}

// confirm renders a yes/no prompt (default No) and the user's scripted answer.
func (h *transcriptSyncHandler) confirm(question string, answer bool) {
	h.Output.Info("? %s [y/N]", question)
	h.Output.Info("› user answers: %s", yesNo(answer))
}

// renderBranchDeletions renders the deletion multi-select using the real option
// labels (via buildDeletionOptions) with [x]/[ ] reflecting the user's final
// selection, followed by an explicit echo of what the user selected.
func (h *transcriptSyncHandler) renderBranchDeletions(branches map[string]string, unpushed map[string]bool, selects []string) {
	if len(branches) == 0 {
		return
	}
	names, options, _ := buildDeletionOptions(branches, unpushed)
	selected := toSet(selects)

	// The deletion prompt belongs to the clean phase; emit that header first so
	// the transcript matches the real ordering (header, then prompt, then items).
	h.ensurePhaseHeader(syncAction.PhaseClean)
	h.Output.Newline()
	h.Output.Info("? Select branches to delete:")
	for i, name := range names {
		mark := " "
		if selected[name] {
			mark = "x"
		}
		h.Output.Info("    [%s] %s", mark, options[i])
	}
	h.Output.Info("› user selects: %s", joinOrNone(sortedCopy(selects)))
}

// step is one action in a scenario, applied to the transcript handler in order.
type step func(h *transcriptSyncHandler)

// emit renders a single sync progress event through the real renderer.
func emit(e syncAction.Event) step {
	return func(h *transcriptSyncHandler) { h.EmitEvent(e) }
}

// phaseStarted emits the phase-start event that prints a phase header.
func phaseStarted(p syncAction.Phase) step {
	return emit(syncAction.Event{Phase: p, Type: syncAction.EventStarted})
}

func trunkFF(rev string) step {
	return emit(syncAction.Event{Phase: syncAction.PhaseTrunk, Type: syncAction.EventCompleted, Branch: "main", NewRevision: rev})
}

func trunkUpToDate() step {
	return emit(syncAction.Event{Phase: syncAction.PhaseTrunk, Type: syncAction.EventCompleted, Branch: "main"})
}

func branchSynced(branch, rev string) step {
	return emit(syncAction.Event{Phase: syncAction.PhaseBranches, Type: syncAction.EventCompleted, Branch: branch, NewRevision: rev})
}

func branchUpToDate(branch string) step {
	return emit(syncAction.Event{Phase: syncAction.PhaseBranches, Type: syncAction.EventCompleted, Branch: branch})
}

func branchDiverged(branch string) step {
	return emit(syncAction.Event{Phase: syncAction.PhaseBranches, Type: syncAction.EventSkipped, Branch: branch, Conflict: true})
}

func githubUpdatingPR(branch string) step {
	return emit(syncAction.Event{Phase: syncAction.PhaseGitHub, Type: syncAction.EventProgress, Branch: branch})
}

func githubMessage(msg string) step {
	return emit(syncAction.Event{Phase: syncAction.PhaseGitHub, Type: syncAction.EventCompleted, Message: msg})
}

func deleted(branch string, pr *int, reason string) step {
	return emit(syncAction.Event{Phase: syncAction.PhaseClean, Type: syncAction.EventCompleted, Branch: branch, PRNumber: pr, Message: reason})
}

func restacked(branch string, pr *int, parent, rev string) step {
	return emit(syncAction.Event{Phase: syncAction.PhaseRestack, Type: syncAction.EventCompleted, Branch: branch, PRNumber: pr, Parent: parent, NewRevision: rev})
}

func restackUpToDate(branch string, pr *int) step {
	return emit(syncAction.Event{Phase: syncAction.PhaseRestack, Type: syncAction.EventCompleted, Branch: branch, PRNumber: pr})
}

func restackConflict(branch string, pr *int) step {
	return emit(syncAction.Event{Phase: syncAction.PhaseRestack, Type: syncAction.EventSkipped, Branch: branch, PRNumber: pr, Conflict: true})
}

// promptDeletions renders the branch-deletion multi-select with the user's
// scripted selection. unpushed lists branches with local commits not yet pushed
// (not pre-selected by default); selects lists what the user finally chooses.
func promptDeletions(branches map[string]string, unpushed, selects []string) step {
	return func(h *transcriptSyncHandler) {
		h.renderBranchDeletions(branches, toSet(unpushed), selects)
	}
}

func promptMetadataConflict(diff *engine.MetadataDiff, accept bool) step {
	return func(h *transcriptSyncHandler) {
		describeMetadataConflict(h.Output, diff)
		h.confirm("Accept remote metadata?", accept)
	}
}

func promptOrphanedMetadata(info engine.OrphanedMetadataInfo, push bool) step {
	return func(h *transcriptSyncHandler) {
		describeOrphanedMetadata(h.Output, info)
		h.confirm("Push your local metadata to remote?", push)
	}
}

func promptResolveConflicts(conflictBranches []string, resolve bool) step {
	return func(h *transcriptSyncHandler) {
		describeRestackConflicts(h.Output, conflictBranches)
		h.confirm("Resolve conflicts now?", resolve)
	}
}

type syncGoldenCase struct {
	name    string
	steps   []step
	summary syncAction.Summary
}

func syncGoldenCases() []syncGoldenCase {
	return []syncGoldenCase{
		{
			name: "up_to_date",
			steps: []step{
				phaseStarted(syncAction.PhaseTrunk),
				trunkUpToDate(),
			},
			summary: syncAction.Summary{UpToDate: true},
		},
		{
			// Phases that start but emit nothing must not print a header.
			name: "empty_phases_suppressed",
			steps: []step{
				phaseStarted(syncAction.PhaseTrunk),
				trunkUpToDate(),
				phaseStarted(syncAction.PhaseBranches), // no items -> header suppressed
				phaseStarted(syncAction.PhaseClean),    // no items -> header suppressed
			},
			summary: syncAction.Summary{UpToDate: true},
		},
		{
			name: "trunk_fast_forwarded",
			steps: []step{
				phaseStarted(syncAction.PhaseTrunk),
				trunkFF("a1b2c3d"),
			},
			summary: syncAction.Summary{TrunkUpdated: true, TrunkRevision: "a1b2c3d"},
		},
		{
			name: "branch_synced",
			steps: []step{
				phaseStarted(syncAction.PhaseTrunk),
				trunkFF("a1b2c3d"),
				phaseStarted(syncAction.PhaseBranches),
				branchSynced("feat-api", "e4f5a6b"),
				branchUpToDate("feat-ui"),
			},
			summary: syncAction.Summary{TrunkUpdated: true, BranchesSynced: 1},
		},
		{
			name: "branch_diverged_skipped",
			steps: []step{
				phaseStarted(syncAction.PhaseTrunk),
				trunkFF("a1b2c3d"),
				phaseStarted(syncAction.PhaseBranches),
				branchDiverged("feat-api"),
			},
			summary: syncAction.Summary{TrunkUpdated: true},
		},
		{
			name: "clean_with_prompt_confirm",
			steps: []step{
				phaseStarted(syncAction.PhaseTrunk),
				trunkFF("a1b2c3d"),
				phaseStarted(syncAction.PhaseClean),
				promptDeletions(
					map[string]string{
						"feat-login": "#123 merged",
						"feat-wip":   "#130 closed",
					},
					[]string{"feat-wip"},   // unpushed — not pre-selected
					[]string{"feat-login"}, // user selects only the merged one
				),
				deleted("feat-login", new(123), "merged"),
			},
			summary: syncAction.Summary{TrunkUpdated: true, BranchesDeleted: 1},
		},
		{
			name: "restack_done",
			steps: []step{
				phaseStarted(syncAction.PhaseTrunk),
				trunkFF("a1b2c3d"),
				phaseStarted(syncAction.PhaseRestack),
				restacked("feat-api", new(201), "main", "b2c3d4e"),
				restacked("feat-ui", new(202), "feat-api", "c3d4e5f"),
				restackUpToDate("feat-docs", nil),
			},
			summary: syncAction.Summary{TrunkUpdated: true, BranchesRestacked: 2},
		},
		{
			name: "restack_conflict_declined",
			steps: []step{
				phaseStarted(syncAction.PhaseRestack),
				restacked("feat-api", new(201), "main", "b2c3d4e"),
				restackConflict("feat-ui", new(202)),
				promptResolveConflicts([]string{"feat-ui"}, false),
			},
			summary: syncAction.Summary{
				BranchesRestacked: 1,
				BranchesSkipped:   1,
				ConflictBranches:  []string{"feat-ui"},
			},
		},
		{
			name: "metadata_conflict_keep_local",
			steps: []step{
				phaseStarted(syncAction.PhaseTrunk),
				trunkFF("a1b2c3d"),
				promptMetadataConflict(&engine.MetadataDiff{
					Branch: "feat-auth",
					Differences: []engine.FieldDiff{
						{Field: "scope", LocalValue: "AUTH", RemoteValue: "AUTHN"},
					},
					RemoteMeta: git.NewMeta().WithLastModifiedBy(&git.ModifiedBy{
						GitName:  "Alex Rivera",
						GitEmail: "alex@example.com",
					}),
				}, false),
			},
			summary: syncAction.Summary{TrunkUpdated: true},
		},
		{
			name: "orphaned_metadata_push",
			steps: []step{
				phaseStarted(syncAction.PhaseTrunk),
				trunkFF("a1b2c3d"),
				promptOrphanedMetadata(engine.OrphanedMetadataInfo{
					BranchName: "feat-wip",
					LocalMeta:  metaWith("WIP", git.LockReasonUser),
				}, true),
			},
			summary: syncAction.Summary{TrunkUpdated: true},
		},
		{
			name: "github_pr_updated",
			steps: []step{
				phaseStarted(syncAction.PhaseTrunk),
				trunkFF("a1b2c3d"),
				phaseStarted(syncAction.PhaseGitHub),
				githubUpdatingPR("feat-api"),
				githubMessage("Updated 1 PR description"),
			},
			summary: syncAction.Summary{TrunkUpdated: true},
		},
		{
			name: "full_sync",
			steps: []step{
				phaseStarted(syncAction.PhaseTrunk),
				trunkFF("9f8e7d6"),
				phaseStarted(syncAction.PhaseGitHub),
				githubMessage("Updated 2 PR descriptions"),
				phaseStarted(syncAction.PhaseBranches),
				branchSynced("feat-api", "e4f5a6b"),
				phaseStarted(syncAction.PhaseClean),
				promptDeletions(
					map[string]string{"feat-old": "#99 merged"},
					nil,
					[]string{"feat-old"},
				),
				deleted("feat-old", new(99), "merged"),
				phaseStarted(syncAction.PhaseRestack),
				restacked("feat-ui", new(202), "feat-api", "c3d4e5f"),
				restackConflict("feat-report", new(203)),
				promptResolveConflicts([]string{"feat-report"}, false),
			},
			summary: syncAction.Summary{
				TrunkUpdated:      true,
				BranchesSynced:    1,
				BranchesRestacked: 1,
				BranchesDeleted:   1,
				BranchesSkipped:   1,
				ConflictBranches:  []string{"feat-report"},
			},
		},
	}
}

func playSyncScenario(c syncGoldenCase) string {
	buf := &bytes.Buffer{}
	h := newTranscriptSyncHandler(output.NewConsoleOutput(buf, false))

	h.Start(0)
	for _, s := range c.steps {
		s(h)
	}
	h.Complete(c.summary)

	return golden.StripANSI(buf.String())
}

func TestSyncGolden(t *testing.T) {
	t.Parallel()
	for _, c := range syncGoldenCases() {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			golden.Assert(t, filepath.Join("testdata", "sync", c.name+".golden"), playSyncScenario(c))
		})
	}
}

// --- small test helpers ---

// answerNo is used to represent a "no" answer in test transcripts.
const answerNo = "no"

func yesNo(b bool) string {
	if b {
		return answerYes
	}
	return answerNo
}

func toSet(xs []string) map[string]bool {
	m := make(map[string]bool, len(xs))
	for _, x := range xs {
		m[x] = true
	}
	return m
}

func sortedCopy(xs []string) []string {
	c := append([]string(nil), xs...)
	sort.Strings(c)
	return c
}

func joinOrNone(xs []string) string {
	if len(xs) == 0 {
		return "(none)"
	}
	return strings.Join(xs, ", ")
}

func metaWith(scope string, lock git.LockReason) *git.Meta {
	m := git.NewMeta()
	if scope != "" {
		m = m.WithScope(&scope)
	}
	if lock != git.LockReasonNone {
		m = m.WithLockReason(lock)
	}
	return m
}
