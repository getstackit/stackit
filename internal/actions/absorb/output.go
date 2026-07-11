package absorb

import (
	"sort"
	"strings"

	"github.com/getstackit/stackit/internal/actions"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/internal/output"
)

// printDryRunOutput prints what would be absorbed in dry-run mode
func printDryRunOutput(hunksByCommit map[string][]git.Hunk, unabsorbedHunks []Unabsorbable, eng engine.Engine, splog output.Output) {
	printAbsorbPreview("Would absorb:", hunksByCommit, unabsorbedHunks, eng, splog)
}

// printAbsorbPlan prints the plan for absorbing changes
func printAbsorbPlan(hunksByCommit map[string][]git.Hunk, unabsorbedHunks []Unabsorbable, eng engine.Engine, splog output.Output) {
	printAbsorbPreview("Will absorb:", hunksByCommit, unabsorbedHunks, eng, splog)
}

func printAbsorbPreview(
	heading string,
	hunksByCommit map[string][]git.Hunk,
	unabsorbedHunks []Unabsorbable,
	eng engine.Engine,
	splog output.Output,
) {
	absorbedCount := 0
	for _, hunks := range hunksByCommit {
		absorbedCount += len(hunks)
	}

	splog.Info(
		"Absorb plan: %d %s into %d %s, %d skipped",
		absorbedCount,
		actions.Pluralize("hunk", absorbedCount),
		len(hunksByCommit),
		actions.Pluralize("commit", len(hunksByCommit)),
		len(unabsorbedHunks),
	)
	splog.Newline()
	splog.Info(heading)

	// Resolve owning branches in one batched scan instead of one lookup per commit.
	commitSHAs := sortedCommitSHAs(hunksByCommit)
	commitBranches := eng.FindBranchesForCommits(commitSHAs)
	for _, commitSHA := range commitSHAs {
		hunks := sortedHunks(hunksByCommit[commitSHA])
		branchName := commitBranches[commitSHA]
		if branchName == "" {
			branchName = unknown
		}

		splog.Info("  %s  %s", shortSHA(commitSHA), output.BranchName(branchName))
		if subject := commitSubject(eng, branchName, commitSHA); subject != "" {
			splog.Info("    %s", subject)
		}
		for _, hunk := range hunks {
			start, end := hunkLineRange(hunk)
			splog.Info("    - %s:%d-%d", hunk.File, start, end)
		}
	}

	if len(unabsorbedHunks) > 0 {
		splog.Newline()
		splog.Warn("Skipped (%d):", len(unabsorbedHunks))

		grouped := groupUnabsorbedByReason(unabsorbedHunks)
		for _, group := range grouped {
			splog.Info("  %s (%d)", group.reason.Description(), len(group.hunks))
			for _, hunk := range sortedHunks(group.hunks) {
				start, end := hunkLineRange(hunk)
				splog.Info("    - %s:%d-%d", hunk.File, start, end)
			}
		}

		tips := tipsForReasons(grouped)
		if len(tips) > 0 {
			splog.Newline()
			splog.Info("Tips:")
			for _, tip := range tips {
				splog.Info("  - %s", tip)
			}
		}
	}
}

type unabsorbedGroup struct {
	reason UnabsorbableReason
	hunks  []git.Hunk
}

func groupUnabsorbedByReason(unabsorbed []Unabsorbable) []unabsorbedGroup {
	byReason := make(map[UnabsorbableReason][]git.Hunk)
	for _, item := range unabsorbed {
		byReason[item.Reason] = append(byReason[item.Reason], item.Hunk)
	}

	reasons := make([]UnabsorbableReason, 0, len(byReason))
	for reason := range byReason {
		reasons = append(reasons, reason)
	}
	sort.Slice(reasons, func(i, j int) bool {
		left := reasonSortIndex(reasons[i])
		right := reasonSortIndex(reasons[j])
		if left == right {
			return reasons[i] < reasons[j]
		}
		return left < right
	})

	groups := make([]unabsorbedGroup, 0, len(reasons))
	for _, reason := range reasons {
		groups = append(groups, unabsorbedGroup{
			reason: reason,
			hunks:  byReason[reason],
		})
	}
	return groups
}

func tipsForReasons(groups []unabsorbedGroup) []string {
	tips := make([]string, 0, len(groups))
	for _, group := range groups {
		tip := group.reason.Tip()
		if tip == "" {
			continue
		}
		tips = append(tips, tip)
	}
	return tips
}

func reasonSortIndex(reason UnabsorbableReason) int {
	switch reason {
	case ReasonCommutesWithAll:
		return 0
	case ReasonNewFile:
		return 1
	case ReasonDeletedFile:
		return 2
	case ReasonBinary:
		return 3
	case ReasonUnknownBranch:
		return 4
	case ReasonUnsupported:
		return 5
	default:
		return 1000
	}
}

func sortedCommitSHAs(hunksByCommit map[string][]git.Hunk) []string {
	commitSHAs := make([]string, 0, len(hunksByCommit))
	for commitSHA := range hunksByCommit {
		commitSHAs = append(commitSHAs, commitSHA)
	}
	sort.Strings(commitSHAs)
	return commitSHAs
}

func sortedHunks(hunks []git.Hunk) []git.Hunk {
	sorted := make([]git.Hunk, len(hunks))
	copy(sorted, hunks)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].File != sorted[j].File {
			return sorted[i].File < sorted[j].File
		}
		if sorted[i].NewStart != sorted[j].NewStart {
			return sorted[i].NewStart < sorted[j].NewStart
		}
		if sorted[i].OldStart != sorted[j].OldStart {
			return sorted[i].OldStart < sorted[j].OldStart
		}
		return sorted[i].Content < sorted[j].Content
	})
	return sorted
}

func hunkLineRange(hunk git.Hunk) (int, int) {
	start := hunk.NewStart
	count := hunk.NewCount
	if count <= 0 || hunk.IsDeletedFile {
		start = hunk.OldStart
		count = hunk.OldCount
	}
	if start <= 0 {
		start = 1
	}
	if count <= 0 {
		count = 1
	}
	return start, start + count - 1
}

func shortSHA(commitSHA string) string {
	if len(commitSHA) <= 8 {
		return commitSHA
	}
	return commitSHA[:8]
}

func commitSubject(eng engine.Engine, branchName, commitSHA string) string {
	if branchName == unknown {
		return ""
	}
	branch := eng.GetBranch(branchName)
	commits, err := branch.GetAllCommits(engine.CommitFormatReadable)
	if err != nil {
		return ""
	}

	for _, commit := range commits {
		fields := strings.Fields(commit)
		if len(fields) == 0 {
			continue
		}
		short := fields[0]
		if strings.HasPrefix(commitSHA, short) || strings.HasPrefix(short, commitSHA) {
			return strings.TrimSpace(strings.TrimPrefix(commit, short))
		}
	}

	return ""
}
