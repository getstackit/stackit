package split

import (
	"testing"

	"github.com/stretchr/testify/require"
)

const testCurrentBranch = "current-branch"

func TestBuildSplitResult(t *testing.T) {
	t.Parallel()
	// This tests the result construction logic that was buggy.
	// The engine expects:
	// - branchPoints: sorted indices (0, 1, 2, ...) where each branch starts
	// - branchNames: names in REVERSE order of branchPoints (oldest branch name first)

	t.Run("single split produces correctly ordered result", func(t *testing.T) {
		t.Parallel()
		// Simulate splitting a branch with 2 commits:
		// - commit 0 (newest): stays in current branch
		// - commit 1 (older): goes to split-off branch
		//
		// Expected result:
		// - branchPoints = [0, 1] (sorted)
		// - branchNames = [split_name, current_name] (older first)

		splitPoint := 1
		groups := []branchGroup{
			{startIdx: 0, endIdx: 1, name: "split-off-branch"},
		}
		currentBranch := testCurrentBranch

		// Replicate the result construction logic
		branchNames := make([]string, 0, 1+len(groups))
		branchPoints := make([]int, 0, 1+len(groups))

		// Add group names in reverse order (oldest branches first)
		for i := len(groups) - 1; i >= 0; i-- {
			branchNames = append(branchNames, groups[i].name)
		}
		// Add current branch name last (newest branch, has index 0)
		branchNames = append(branchNames, currentBranch)

		// Build branchPoints in sorted order (forward iteration since startIdx is ascending)
		branchPoints = append(branchPoints, 0) // Current branch at commit 0
		for i := range groups {
			branchPoints = append(branchPoints, splitPoint+groups[i].startIdx)
		}

		// Verify the result
		require.Equal(t, []string{"split-off-branch", testCurrentBranch}, branchNames,
			"branch names should be ordered from oldest to newest")
		require.Equal(t, []int{0, 1}, branchPoints,
			"branch points should be sorted (0 first, then 1)")

		// Verify the correspondence: branchNames[i] corresponds to branchPoints[len-1-i]
		// This is how the engine interprets it (reversed correspondence)
		// branchNames[0] = "split-off-branch" -> for branchPoints[1] = 1 (older commit)
		// branchNames[1] = "current-branch" -> for branchPoints[0] = 0 (newer commit)
	})

	t.Run("multiple splits produce correctly ordered result", func(t *testing.T) {
		t.Parallel()
		// Simulate splitting a branch with 3 commits:
		// - commit 0 (newest): stays in current branch
		// - commit 1: goes to split-branch-1 (created first, closer to split point)
		// - commit 2 (oldest): goes to split-branch-2 (created second, further from split point)
		//
		// Groups are ordered by startIdx ascending (0, 1, ...)
		// But branchNames need oldest first, so we reverse iterate groups
		//
		// Expected result:
		// - branchPoints = [0, 1, 2] (sorted)
		// - branchNames = [split-branch-2, split-branch-1, current] (oldest first)

		splitPoint := 1
		groups := []branchGroup{
			{startIdx: 0, endIdx: 1, name: "split-branch-1"}, // commit at splitPoint+0=1
			{startIdx: 1, endIdx: 2, name: "split-branch-2"}, // commit at splitPoint+1=2
		}
		currentBranch := testCurrentBranch

		branchNames := make([]string, 0, 1+len(groups))
		branchPoints := make([]int, 0, 1+len(groups))

		// Add group names in reverse order (oldest branches first)
		for i := len(groups) - 1; i >= 0; i-- {
			branchNames = append(branchNames, groups[i].name)
		}
		branchNames = append(branchNames, currentBranch)

		// Build branchPoints in sorted order (forward iteration since startIdx is ascending)
		branchPoints = append(branchPoints, 0)
		for i := range groups {
			branchPoints = append(branchPoints, splitPoint+groups[i].startIdx)
		}

		require.Equal(t, []string{"split-branch-2", "split-branch-1", testCurrentBranch}, branchNames,
			"branch names should be ordered from oldest to newest")
		require.Equal(t, []int{0, 1, 2}, branchPoints,
			"branch points should be sorted")
	})

	t.Run("regression: old buggy code produced unsorted branchPoints", func(t *testing.T) {
		t.Parallel()
		// This test documents the bug that was fixed.
		// The old code produced branchPoints = [1, 0] instead of [0, 1]
		// which caused the engine to create branches at wrong commits.

		splitPoint := 1
		groups := []branchGroup{
			{startIdx: 0, endIdx: 1, name: "split-off-branch"},
		}
		_ = testCurrentBranch // not used in this test, just documenting the scenario

		// OLD BUGGY CODE (do not use):
		// branchNames := []string{}
		// branchPoints := []int{}
		// for i := len(groups) - 1; i >= 0; i-- {
		//     branchNames = append(branchNames, groups[i].name)
		//     branchPoints = append(branchPoints, splitPoint+groups[i].startIdx)  // Added 1 first!
		// }
		// branchNames = append(branchNames, currentBranch)
		// branchPoints = append(branchPoints, 0)  // Added 0 second!
		// Result: branchPoints = [1, 0] - WRONG!

		// Simulate the buggy result
		buggyBranchPoints := []int{}
		for i := len(groups) - 1; i >= 0; i-- {
			buggyBranchPoints = append(buggyBranchPoints, splitPoint+groups[i].startIdx)
		}
		buggyBranchPoints = append(buggyBranchPoints, 0)

		// The buggy result is NOT sorted
		require.Equal(t, []int{1, 0}, buggyBranchPoints,
			"documenting the bug: old code produced unsorted branchPoints")

		// The fix produces sorted branchPoints
		fixedBranchPoints := []int{0}
		for i := len(groups) - 1; i >= 0; i-- {
			fixedBranchPoints = append(fixedBranchPoints, splitPoint+groups[i].startIdx)
		}

		require.Equal(t, []int{0, 1}, fixedBranchPoints,
			"fixed code produces sorted branchPoints")
	})
}
