package sync

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestDryRunPlanResult locks the JSON contract: the rich preview plan must
// project to a names-only DryRunResult, dropping the display detail (target
// revision, deletion reason, restack parent) that only the text preview uses.
func TestDryRunPlanResult(t *testing.T) {
	t.Parallel()
	plan := DryRunPlan{
		PullBranch:    "main",
		PullRevision:  "abc1234",
		Clean:         []DryRunCleanItem{{Branch: "feat-old", Reason: "merged into main"}},
		Restack:       []DryRunRestackItem{{Branch: "feat-api", Parent: "main"}},
		RestackStacks: []string{"feat-api"},
		SkippedStacks: []string{"feat-wip"},
	}
	result := plan.Result()
	require.Equal(t, "main", result.WouldPull)
	require.Equal(t, []string{"feat-old"}, result.WouldClean)
	require.Equal(t, []string{"feat-api"}, result.WouldRestack)
	require.Equal(t, []string{"feat-api"}, result.WouldRestackStacks)
	require.Equal(t, []string{"feat-wip"}, result.SkippedStacks)
}
