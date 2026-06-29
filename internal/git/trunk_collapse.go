package git

// CollapseStackMerges returns the input commits with constituent-PR commits
// dropped when their PR number is already represented by a stack-merge
// consolidation commit in the same slice. Input order is preserved.
//
// It is the dedup half shared by the HTTP "recently merged" mapper and the
// `stackit log` command, so both collapse consolidated stacks identically.
// Presentation shaping (terminal vs JSX) stays with each caller; the PR-title
// enrichment they both need lives in PRTitleNumbers / CollapsedMessage /
// ConstituentPRTitles below.
func CollapseStackMerges(commits []RecentCommit) []RecentCommit {
	// Collect all PR numbers covered by stack-merge consolidation commits.
	coveredPRs := make(map[int]struct{})
	for _, c := range commits {
		if c.StackSize > 0 {
			for _, pr := range c.StackPRNumbers {
				coveredPRs[pr] = struct{}{}
			}
		}
	}

	result := make([]RecentCommit, 0, len(commits))
	for _, c := range commits {
		// Skip commits whose PR is already represented by a stack-merge.
		if c.PRNumber != 0 && c.StackSize == 0 {
			if _, covered := coveredPRs[c.PRNumber]; covered {
				continue
			}
		}
		result = append(result, c)
	}
	return result
}

// PRTitleNumbers returns the unique PR numbers whose titles are needed to enrich
// the given commits: for each stack-merge, its consolidation PR plus its
// constituent PRs. Regular (non-stack) commits contribute nothing — their PR
// title is never displayed — so callers don't over-fetch. Order is first-seen
// stable. Safe to call on either the raw or the collapsed slice: collapse only
// drops covered regular commits, never stack-merges, so both yield the same set.
func PRTitleNumbers(commits []RecentCommit) []int {
	seen := make(map[int]struct{})
	var nums []int
	add := func(n int) {
		if n == 0 {
			return
		}
		if _, ok := seen[n]; ok {
			return
		}
		seen[n] = struct{}{}
		nums = append(nums, n)
	}
	for _, c := range commits {
		if c.StackSize == 0 {
			continue
		}
		add(c.PRNumber)
		for _, pr := range c.StackPRNumbers {
			add(pr)
		}
	}
	return nums
}

// CollapsedMessage returns the display message for a commit: a stack-merge uses
// its consolidation PR's title when available, replacing the raw
// "Merge pull request #N from ..." subject; everything else falls back to the
// commit subject.
func CollapsedMessage(c RecentCommit, prTitles map[int]string) string {
	if c.StackSize > 0 && c.PRNumber != 0 && len(prTitles) > 0 {
		if title, ok := prTitles[c.PRNumber]; ok {
			return title
		}
	}
	return c.Subject
}

// ConstituentPRTitles returns the subset of prTitles keyed by a stack-merge's
// constituent PR numbers, or nil when the commit is not a stack-merge or no
// titles apply.
func ConstituentPRTitles(c RecentCommit, prTitles map[int]string) map[int]string {
	if c.StackSize == 0 || len(prTitles) == 0 {
		return nil
	}
	titles := make(map[int]string)
	for _, pr := range c.StackPRNumbers {
		if title, ok := prTitles[pr]; ok {
			titles[pr] = title
		}
	}
	if len(titles) == 0 {
		return nil
	}
	return titles
}
