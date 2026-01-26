package actions

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSortBranchesByStackOrder(t *testing.T) {
	t.Parallel()

	t.Run("sorts linear stack correctly", func(t *testing.T) {
		t.Parallel()

		branches := []BranchHealth{
			{Name: "c", Parent: "b"},
			{Name: "a", Parent: "main"},
			{Name: "b", Parent: "a"},
		}

		sortBranchesByStackOrder(branches)

		require.Equal(t, "a", branches[0].Name)
		require.Equal(t, "b", branches[1].Name)
		require.Equal(t, "c", branches[2].Name)
	})

	t.Run("handles multiple roots", func(t *testing.T) {
		t.Parallel()

		branches := []BranchHealth{
			{Name: "feature-b", Parent: "main"},
			{Name: "feature-a", Parent: "main"},
			{Name: "feature-a-child", Parent: "feature-a"},
		}

		sortBranchesByStackOrder(branches)

		// Roots should be sorted alphabetically, then children follow
		require.Equal(t, "feature-a", branches[0].Name)
		require.Equal(t, "feature-a-child", branches[1].Name)
		require.Equal(t, "feature-b", branches[2].Name)
	})

	t.Run("handles cycle without infinite loop", func(t *testing.T) {
		t.Parallel()

		// Create a cycle: a -> b -> c -> a
		branches := []BranchHealth{
			{Name: "a", Parent: "c"},
			{Name: "b", Parent: "a"},
			{Name: "c", Parent: "b"},
		}

		// This should not hang - the visited set should prevent infinite loop
		sortBranchesByStackOrder(branches)

		// All branches should be present (order may vary due to cycle)
		names := make(map[string]bool)
		for _, b := range branches {
			names[b.Name] = true
		}
		require.True(t, names["a"])
		require.True(t, names["b"])
		require.True(t, names["c"])
	})

	t.Run("handles self-referential parent", func(t *testing.T) {
		t.Parallel()

		branches := []BranchHealth{
			{Name: "a", Parent: "a"}, // Self-referential
			{Name: "b", Parent: "main"},
		}

		sortBranchesByStackOrder(branches)

		// Should not hang, all branches should be present
		require.Len(t, branches, 2)
	})

	t.Run("handles empty input", func(t *testing.T) {
		t.Parallel()

		branches := []BranchHealth{}
		sortBranchesByStackOrder(branches)
		require.Empty(t, branches)
	})

	t.Run("handles orphan branches", func(t *testing.T) {
		t.Parallel()

		// Branches with parents not in the list
		branches := []BranchHealth{
			{Name: "orphan-a", Parent: "deleted-branch"},
			{Name: "orphan-b", Parent: "another-deleted"},
			{Name: "child-of-orphan", Parent: "orphan-a"},
		}

		sortBranchesByStackOrder(branches)

		// Orphans are treated as roots, sorted alphabetically
		require.Equal(t, "orphan-a", branches[0].Name)
		require.Equal(t, "child-of-orphan", branches[1].Name)
		require.Equal(t, "orphan-b", branches[2].Name)
	})
}

func TestGenerateRecommendations(t *testing.T) {
	t.Parallel()

	t.Run("recommends restack when branches need it", func(t *testing.T) {
		t.Parallel()

		branches := []BranchHealth{
			{Name: "a", NeedsRestack: true},
			{Name: "b", NeedsRestack: true},
			{Name: "c", NeedsRestack: false},
		}

		recs := generateRecommendations(branches)

		var restackRec *Recommendation
		for i := range recs {
			if recs[i].Action == "restack" {
				restackRec = &recs[i]
				break
			}
		}
		require.NotNil(t, restackRec)
		require.Contains(t, restackRec.Reason, "2 branch(es)")
		require.Equal(t, "stackit restack", restackRec.Command)
	})

	t.Run("recommends sync for stale branches", func(t *testing.T) {
		t.Parallel()

		branches := []BranchHealth{
			{Name: "a", CommitsBehind: StaleCommitThreshold + 1},
			{Name: "b", CommitsBehind: StaleCommitThreshold + 10},
			{Name: "c", CommitsBehind: 5}, // Not stale
		}

		recs := generateRecommendations(branches)

		var syncRec *Recommendation
		for i := range recs {
			if recs[i].Action == "sync" {
				syncRec = &recs[i]
				break
			}
		}
		require.NotNil(t, syncRec)
		require.Contains(t, syncRec.Reason, "2 branch(es)")
		require.Contains(t, syncRec.Reason, "30 commits") // max behind = StaleCommitThreshold + 10
	})

	t.Run("recommends fix_ci for failing branches", func(t *testing.T) {
		t.Parallel()

		branches := []BranchHealth{
			{Name: "a", CI: CIStatusFailing, CIError: "test-suite"},
			{Name: "b", CI: CIStatusPassing},
		}

		recs := generateRecommendations(branches)

		var ciRec *Recommendation
		for i := range recs {
			if recs[i].Action == "fix_ci" {
				ciRec = &recs[i]
				break
			}
		}
		require.NotNil(t, ciRec)
		require.Equal(t, "a", ciRec.Branch)
		require.Contains(t, ciRec.Reason, "test-suite")
		require.Equal(t, 1, ciRec.Priority) // High priority
	})

	t.Run("recommends merge for approved branches with passing CI", func(t *testing.T) {
		t.Parallel()

		branches := []BranchHealth{
			{Name: "ready", PRStatus: PRStatusApproved, CI: CIStatusPassing},
			{Name: "not-ready", PRStatus: PRStatusOpen, CI: CIStatusPassing},
		}

		recs := generateRecommendations(branches)

		var mergeRec *Recommendation
		for i := range recs {
			if recs[i].Action == "merge" {
				mergeRec = &recs[i]
				break
			}
		}
		require.NotNil(t, mergeRec)
		require.Equal(t, "ready", mergeRec.Branch)
		require.Equal(t, "stackit merge ready", mergeRec.Command)
	})

	t.Run("recommends submit for branches without PRs", func(t *testing.T) {
		t.Parallel()

		branches := []BranchHealth{
			{Name: "no-pr", PRStatus: PRStatusNone},
			{Name: "has-pr", PRStatus: PRStatusOpen},
			{Name: "locked-no-pr", PRStatus: PRStatusNone, IsLocked: true}, // Should be excluded
		}

		recs := generateRecommendations(branches)

		var submitRec *Recommendation
		for i := range recs {
			if recs[i].Action == "submit" {
				submitRec = &recs[i]
				break
			}
		}
		require.NotNil(t, submitRec)
		require.Contains(t, submitRec.Reason, "1 branch(es)") // Only no-pr, not locked
	})

	t.Run("sorts recommendations by priority", func(t *testing.T) {
		t.Parallel()

		branches := []BranchHealth{
			{Name: "a", PRStatus: PRStatusNone},                          // Priority 3 (submit)
			{Name: "b", CI: CIStatusFailing},                             // Priority 1 (fix_ci)
			{Name: "c", NeedsRestack: true},                              // Priority 2 (restack)
			{Name: "d", PRStatus: PRStatusApproved, CI: CIStatusPassing}, // Priority 3 (merge)
			{Name: "e", CommitsBehind: StaleCommitThreshold + 1},         // Priority 2 (sync)
		}

		recs := generateRecommendations(branches)

		// First should be high priority (1)
		require.Equal(t, 1, recs[0].Priority)
		// Verify overall ordering
		for i := 1; i < len(recs); i++ {
			require.GreaterOrEqual(t, recs[i].Priority, recs[i-1].Priority)
		}
	})

	t.Run("returns empty for healthy stack", func(t *testing.T) {
		t.Parallel()

		branches := []BranchHealth{
			{Name: "a", PRStatus: PRStatusOpen, CI: CIStatusPassing, NeedsRestack: false, CommitsBehind: 5},
			{Name: "b", PRStatus: PRStatusOpen, CI: CIStatusPassing, NeedsRestack: false, CommitsBehind: 3},
		}

		recs := generateRecommendations(branches)
		require.Empty(t, recs)
	})
}
