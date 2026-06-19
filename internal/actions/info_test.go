package actions

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

// TestSingleBranchInfoFilesChangedUsesDivergenceBase verifies the single-branch
// info path counts changed files against the branch's divergence point — the
// same base as its additions/deletions — not the parent's current tip, so the
// count stays stable when the parent advances without a restack.
func TestSingleBranchInfoFilesChangedUsesDivergenceBase(t *testing.T) {
	t.Parallel()
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
	s.WithLinearStack3() // main -> a -> b -> c

	before := singleBranchFilesChanged(t, s, "b")
	require.GreaterOrEqual(t, before, 1)

	// Advance b's parent (a) with a commit touching a different file, without
	// restacking b, so a's current tip moves past b's divergence point.
	s.Checkout("a").CommitChange("a-advanced.txt", "advance a past b's divergence")

	after := singleBranchFilesChanged(t, s, "b")
	require.Equal(t, before, after,
		"single-branch files-changed must use the divergence base, not the parent's current tip")
}

func singleBranchFilesChanged(t *testing.T, s *scenario.Scenario, name string) int {
	t.Helper()
	s.Output.Reset()
	require.NoError(t, outputBranchInfoJSON(s.Context, s.Engine.GetBranch(name)))

	var info SingleBranchInfo
	require.NoError(t, json.Unmarshal(s.Output.Bytes(), &info))
	return info.DiffStats.FilesChanged
}
