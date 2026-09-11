package engine

import (
	"context"
	"strconv"
	"strings"

	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/internal/utils"
)

type cachedDiffStat struct {
	DiffStat
	Files int
}

func parseDiffStat(output string) cachedDiffStat {
	var stat cachedDiffStat
	for line := range strings.SplitSeq(output, "\n") {
		fields := strings.SplitN(line, "\t", 3)
		if len(fields) != 3 {
			continue
		}
		added, _ := strconv.Atoi(fields[0])
		deleted, _ := strconv.Atoi(fields[1])
		stat.Added += added
		stat.Deleted += deleted
		stat.Files++ // Binary changes and renames each count as one file too.
	}
	return stat
}

func (e *engineImpl) branchStatRanges(branches Branches) map[string]git.RevRange {
	return batchByBranch(e, branches, func(b Branch, head, parentRev, storedBase string) git.RevRange {
		base := statBase(parentRev, storedBase)
		if e.IsTrunk(b) {
			base = head
		}
		return git.RevRange{Base: base, Head: head}
	})
}

// readDiffStats shares numstat data between line stats and file counts. Only
// distinct, uncached ranges go to Git; failed reads never poison the cache.
func (e *engineImpl) readDiffStats(ctx context.Context, ranges map[string]git.RevRange) map[git.RevRange]cachedDiffStat {
	result := make(map[git.RevRange]cachedDiffStat, len(ranges))
	seen := make(map[git.RevRange]bool, len(ranges))
	var missing []git.RevRange
	for _, rr := range ranges {
		if seen[rr] || rr.Base == rr.Head || rr.Base == "" || rr.Head == "" {
			continue
		}
		seen[rr] = true
		if stat, ok := e.diffStatsCache.Load(rr); ok {
			result[rr] = stat.(cachedDiffStat)
		} else {
			missing = append(missing, rr)
		}
	}
	var batch map[git.RevRange]string
	if len(missing) > 1 {
		batch, _ = e.git.BatchDiffNumstat(ctx, missing)
	}
	utils.Run(missing, func(rr git.RevRange) {
		output, ok := batch[rr]
		if !ok {
			// Keep single-range reads cheap and isolate failures when a batch
			// fails (for example, one stored divergence object was pruned).
			var err error
			output, err = e.git.RunGitCommandWithContext(ctx, "diff", "--numstat", rr.Base, rr.Head)
			if err != nil {
				return
			}
		}
		e.diffStatsCache.Store(rr, parseDiffStat(output))
	})
	for _, rr := range missing {
		if stat, ok := e.diffStatsCache.Load(rr); ok {
			result[rr] = stat.(cachedDiffStat)
		}
	}
	return result
}
