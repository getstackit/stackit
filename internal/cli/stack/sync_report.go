package stack

import (
	"fmt"

	syncAction "github.com/getstackit/stackit/internal/actions/sync"
	"github.com/getstackit/stackit/internal/cli/common"
	"github.com/getstackit/stackit/internal/tui/style"
)

// Sync's output answers three questions a developer cannot answer for
// themselves after the world moved underneath them:
//
//  1. What landed? Their work merged, or a teammate's did.
//  2. Did the shape of their stack change? A parent landed, so its children were
//     reparented onto something else.
//  3. What needs them now? A conflict.
//
// Everything else — trunk pulled, PR info fetched, five branches rebased exactly
// as expected — is stackit confirming it did its job, and belongs in a count.
//
// So a phase's header states its outcome ("Restacked 5 branches on main +14")
// rather than announcing its intent ("Restacking branches..."), and only rows
// carrying one of the three answers above are printed. A header cannot be
// written until its phase is over, so rows buffer into a phaseBlock and flush
// when the next phase starts.
//
// Headers carry no emoji. A 📥 on a header says nothing a developer needs — the
// words already say what happened, and indentation already separates a header
// from its rows. The single-width item glyphs stay, because those do carry
// information: ↳ is a consequence, ⚠ is something needing you.

// rowMark selects the glyph leading a report row.
type rowMark int

const (
	// markNone is a plain indented row: an item in a list the header already
	// characterized, such as which branches landed.
	markNone rowMark = iota
	// markNote flags a change to the stack's shape.
	markNote
	// markWarn flags something needing the developer's attention.
	markWarn
)

// reportRow is one line worth keeping under a phase header.
type reportRow struct {
	Text string
	Mark rowMark
}

// phaseBlock is a finished phase: its outcome header and the rows that survived.
// An empty Header means the phase produced nothing worth saying.
type phaseBlock struct {
	Header string
	Rows   []reportRow
}

// Empty reports whether this block should be printed at all.
func (b phaseBlock) Empty() bool { return b.Header == "" && len(b.Rows) == 0 }

// syncReport accumulates events into per-phase blocks. It holds the run-wide
// trunk facts too, because the restack header reports what the stack was rebased
// onto — which the trunk phase learned, several phases earlier.
//
// Tallies are kept per phase rather than one-at-a-time because sync runs the
// trunk fetch and the GitHub refresh concurrently: both announce themselves
// before either finishes, so "the phase in flight" is not a single value. A
// phase is instead closed out when the pipeline demonstrably moved past it —
// when an event arrives for a later phase — which is also what orders the
// output.
type syncReport struct {
	trunkName    string
	trunkCommits int

	tallies map[syncAction.Phase]*phaseTally
	flushed map[syncAction.Phase]bool
}

// reportPhaseOrder is the pipeline sync walks. It orders the output and decides
// when a phase can be closed out.
var reportPhaseOrder = []syncAction.Phase{
	syncAction.PhaseTrunk,
	syncAction.PhaseGitHub,
	syncAction.PhaseBranches,
	syncAction.PhaseClean,
	syncAction.PhaseRestack,
}

func phaseRank(phase syncAction.Phase) int {
	for i, p := range reportPhaseOrder {
		if p == phase {
			return i
		}
	}
	return len(reportPhaseOrder)
}

// phaseTally is the running count for one phase.
type phaseTally struct {
	trunkBranch    string
	trunkRev       string
	githubMessage  string
	synced         int
	deleted        int
	restacked      int
	rerereResolved int
	rows           []reportRow
}

func (r *syncReport) tally(phase syncAction.Phase) *phaseTally {
	if r.tallies == nil {
		r.tallies = map[syncAction.Phase]*phaseTally{}
		r.flushed = map[syncAction.Phase]bool{}
	}
	if r.tallies[phase] == nil {
		r.tallies[phase] = &phaseTally{}
	}
	return r.tallies[phase]
}

// Add folds one event into its phase's tally and returns any earlier phases the
// pipeline has now moved past, ready to print.
func (r *syncReport) Add(event syncAction.Event) []phaseBlock {
	done := r.flushBefore(event.Phase)

	switch event.Phase {
	case syncAction.PhaseTrunk:
		r.addTrunk(event)
	case syncAction.PhaseGitHub:
		r.addGitHub(event)
	case syncAction.PhaseBranches:
		r.addBranchSync(event)
	case syncAction.PhaseClean:
		r.addClean(event)
	case syncAction.PhaseRestack:
		r.addRestack(event)
	}
	return done
}

// flushBefore closes out every phase earlier in the pipeline than phase.
func (r *syncReport) flushBefore(phase syncAction.Phase) []phaseBlock {
	limit := phaseRank(phase)
	var blocks []phaseBlock
	for i, p := range reportPhaseOrder {
		if i >= limit {
			break
		}
		if block, ok := r.close(p); ok {
			blocks = append(blocks, block)
		}
	}
	return blocks
}

// Flush closes out every phase that has not been printed yet, in pipeline order.
func (r *syncReport) Flush() []phaseBlock {
	var blocks []phaseBlock
	for _, p := range reportPhaseOrder {
		if block, ok := r.close(p); ok {
			blocks = append(blocks, block)
		}
	}
	return blocks
}

// close renders one phase's block, once. It reports false when the phase has
// already been printed or had nothing worth saying.
func (r *syncReport) close(phase syncAction.Phase) (phaseBlock, bool) {
	if r.flushed[phase] || r.tallies[phase] == nil {
		return phaseBlock{}, false
	}
	r.flushed[phase] = true
	block := phaseBlock{Header: r.header(phase), Rows: r.tallies[phase].rows}
	if block.Empty() {
		return phaseBlock{}, false
	}
	return block, true
}

func (r *syncReport) addTrunk(event syncAction.Event) {
	if event.Type != syncAction.EventCompleted {
		return
	}
	// Remember trunk regardless of whether it moved: the restack header names it.
	r.trunkName = event.Branch
	// A held trunk reports no new revision, the same as one that needed no
	// work. Say which one happened — the remedy is in another directory the
	// user is not looking at.
	if event.HeldBy != "" {
		r.warn(syncAction.PhaseTrunk, "Held %s back: %s",
			style.ColorBranchName(event.Branch), event.HeldBy)
		return
	}
	if event.NewRevision == "" {
		return
	}
	t := r.tally(syncAction.PhaseTrunk)
	t.trunkBranch = event.Branch
	t.trunkRev = event.NewRevision
	r.trunkCommits = event.Commits
}

func (r *syncReport) addGitHub(event syncAction.Event) {
	// In-flight rows are dropped: they are emitted for every branch before the
	// work runs and each is superseded by the completion message.
	if event.Type == syncAction.EventCompleted && event.Message != "" {
		r.tally(syncAction.PhaseGitHub).githubMessage = event.Message
	}
}

func (r *syncReport) addBranchSync(event syncAction.Event) {
	switch event.Type {
	case syncAction.EventCompleted:
		// Only a branch that actually moved counts; already-current is the default.
		if event.NewRevision != "" {
			r.tally(syncAction.PhaseBranches).synced++
		}
	case syncAction.EventSkipped:
		// Someone else pushed to this branch — the developer has to decide.
		if event.Conflict {
			r.warn(syncAction.PhaseBranches, "%s diverged from remote — left alone", branchLabel(event))
		}
	}
}

func (r *syncReport) addClean(event syncAction.Event) {
	if event.Type != syncAction.EventCompleted || event.Branch == "" {
		return
	}
	r.tally(syncAction.PhaseClean).deleted++
	// Landing is news, so each landed branch is named. The PR number leads: it is
	// the part a developer can act on.
	name := style.ColorBranchName(style.DisplayBranchName(event.Branch))
	if event.PRNumber != nil {
		name = fmt.Sprintf("%s %s", style.ColorDim(fmt.Sprintf("#%d", *event.PRNumber)), name)
	}
	t := r.tally(syncAction.PhaseClean)
	t.rows = append(t.rows, reportRow{Text: name, Mark: markNone})
}

func (r *syncReport) addRestack(event syncAction.Event) {
	if event.Branch == "" {
		return
	}

	// A reparent can accompany any outcome, and it is the one thing a developer
	// cannot infer, so it is reported before whatever else happened to the branch.
	if event.Reparented {
		r.note(syncAction.PhaseRestack, "%s reparented onto %s (%s landed)",
			branchLabel(event),
			style.ColorBranchName(style.DisplayBranchName(event.NewParent)),
			style.DisplayBranchName(event.OldParent))
	}

	switch event.Type {
	case syncAction.EventCompleted:
		switch {
		case event.HeldBy != "":
			// A hold reports the same status as "nothing to do", so say which
			// one happened — the remedy lives in another worktree.
			r.warn(syncAction.PhaseRestack, "Held %s back: %s", branchLabel(event), event.HeldBy)
		case event.IsLocked():
			r.warn(syncAction.PhaseRestack, "%s %s: %s", branchLabel(event), common.ReasonLocked, event.LockReason)
		case event.Frozen:
			r.warn(syncAction.PhaseRestack, "%s %s", branchLabel(event), common.ReasonFrozen)
		case event.NewRevision != "":
			// A successful rebase is what the developer already assumed would
			// happen. It is a number in the header, not a line.
			r.tally(syncAction.PhaseRestack).restacked++
		}
		// Conflicts git resolved from recorded resolutions are worth surfacing:
		// the developer did not resolve them this time and may want to check.
		r.tally(syncAction.PhaseRestack).rerereResolved += event.RerereResolvedCount
	case syncAction.EventSkipped:
		if event.Conflict {
			r.warn(syncAction.PhaseRestack, "%s conflict — needs resolving", branchLabel(event))
			return
		}
		r.warn(syncAction.PhaseRestack, "%s %s", branchLabel(event), event.Message)
	}
}

func (r *syncReport) note(phase syncAction.Phase, format string, args ...any) {
	t := r.tally(phase)
	t.rows = append(t.rows, reportRow{Text: fmt.Sprintf(format, args...), Mark: markNote})
}

func (r *syncReport) warn(phase syncAction.Phase, format string, args ...any) {
	t := r.tally(phase)
	t.rows = append(t.rows, reportRow{Text: fmt.Sprintf(format, args...), Mark: markWarn})
}

// header states what the phase in flight accomplished, or "" when it did
// nothing worth reporting.
func (r *syncReport) header(phase syncAction.Phase) string {
	t := r.tally(phase)
	switch phase {
	case syncAction.PhaseTrunk:
		if t.trunkRev == "" {
			return ""
		}
		name := style.ColorBranchName(style.DisplayBranchName(t.trunkBranch))
		// The commit count is the interesting part — how far the world moved. If
		// it could not be determined, at least say the pull happened.
		if r.trunkCommits <= 0 {
			return header("Pulled %s", name)
		}
		return header("%s%s", name, commitDelta(r.trunkCommits))
	case syncAction.PhaseGitHub:
		if t.githubMessage == "" {
			return ""
		}
		return header("%s", t.githubMessage)
	case syncAction.PhaseBranches:
		if t.synced == 0 {
			return headerForRowsOnly(t.rows, "Stack branches")
		}
		return header("Pulled %s from remote", countBranches(t.synced))
	case syncAction.PhaseClean:
		if t.deleted == 0 {
			return ""
		}
		return header("%s landed and cleaned up", countBranches(t.deleted))
	case syncAction.PhaseRestack:
		if t.restacked == 0 {
			return headerForRowsOnly(t.rows, "Restack")
		}
		return header("Restacked %s%s%s",
			countBranches(t.restacked), r.restackBase(), r.rerereSuffix(t))
	}
	return ""
}

// rerereSuffix notes conflicts git replayed from recorded resolutions.
func (r *syncReport) rerereSuffix(t *phaseTally) string {
	if t.rerereResolved == 0 {
		return ""
	}
	return style.ColorDim(fmt.Sprintf(" · %d conflict(s) auto-resolved by rerere", t.rerereResolved))
}

// restackBase names what the stack was rebased onto, but only when trunk moved.
// If trunk stood still, "on main" adds nothing a developer did not assume.
func (r *syncReport) restackBase() string {
	if r.trunkCommits == 0 || r.trunkName == "" {
		return ""
	}
	return fmt.Sprintf(" on %s%s",
		style.ColorBranchName(style.DisplayBranchName(r.trunkName)),
		commitDelta(r.trunkCommits))
}

// headerForRowsOnly gives rows a header to sit under when the phase's happy
// path produced no count — a phase whose only news is a conflict still needs to
// say which phase it was.
func headerForRowsOnly(rows []reportRow, text string) string {
	if len(rows) == 0 {
		return ""
	}
	return header("%s", text)
}

// header renders a phase header. Bold is the whole treatment: it separates the
// header from its indented rows without spending a glyph on decoration.
func header(format string, args ...any) string {
	return style.Bold(fmt.Sprintf(format, args...))
}

func commitDelta(commits int) string {
	if commits <= 0 {
		return ""
	}
	return " " + style.ColorDim(fmt.Sprintf("+%d", commits))
}

func countBranches(n int) string {
	if n == 1 {
		return "1 branch"
	}
	return fmt.Sprintf("%d branches", n)
}

// branchLabel renders a branch for a report row: shortened, colored, and marked
// when it is the branch the developer is standing on, with its PR number.
func branchLabel(event syncAction.Event) string {
	label := style.ColorBranchNameIf(style.DisplayBranchName(event.Branch), event.IsCurrent)
	if event.PRNumber != nil {
		label += " " + style.ColorDim(fmt.Sprintf("#%d", *event.PRNumber))
	}
	return label
}

// glyph returns the leading marker for a row, padded so every row's text starts
// in the same column whether or not it carries a glyph.
func (m rowMark) glyph() string {
	switch m {
	case markNote:
		return style.MarkNote()
	case markWarn:
		return style.MarkWarning()
	default:
		return " "
	}
}
