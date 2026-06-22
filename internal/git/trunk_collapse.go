package git

// CollapseStackMerges returns the input commits with constituent-PR commits
// dropped when their PR number is already represented by a stack-merge
// consolidation commit in the same slice. Input order is preserved.
//
// It is the dedup half shared by the HTTP "recently merged" mapper and the
// `stackit log` command, so both collapse consolidated stacks identically.
// PR-title enrichment and presentation shaping stay with each caller.
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
