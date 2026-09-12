package stack_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/actions/submit"
	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

func TestSubmitRegeneratePreview(t *testing.T) {
	t.Parallel()
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).WithInProcess(true)
	s.CreateBranch("feature").CommitChange("first", "feat: refreshed\n\nUpdated description").TrackBranch("feature", "main")
	for _, command := range []string{"submit", "ss"} {
		output, err := s.RunCliAndGetOutput(command, "--regenerate", "--no-edit", "--dry-run", "--json")
		require.NoError(t, err, output)
		var result submit.JSONResult
		require.NoError(t, json.Unmarshal([]byte(output), &result))
		require.Equal(t, submit.OutcomeDryRun, result.Outcome)
		require.Len(t, result.Branches, 1)
		require.NotNil(t, result.Branches[0].Regenerated)
		require.Equal(t, "feat: refreshed", result.Branches[0].Regenerated.Title)
		require.Contains(t, result.Branches[0].Regenerated.Body, "Updated description")
	}
	output, err := s.RunCliAndGetOutput("submit", "--regenerate", "--no-edit", "--dry-run")
	require.NoError(t, err, output)
	require.Contains(t, output, "PR title: feat: refreshed")
	require.Contains(t, output, "Updated description")
	// The multi-branch plan uses a different renderer from the single-branch plan.
	s.CreateBranch("child").CommitChange("second", "fix: child").TrackBranch("child", "feature")
	output, err = s.RunCliAndGetOutput("submit", "--regenerate", "--no-edit", "--dry-run")
	require.NoError(t, err, output)
	require.Contains(t, output, "PR title: feat: refreshed")
	require.Contains(t, output, "PR title: fix: child")
	require.Contains(t, output, "(empty)")
}
