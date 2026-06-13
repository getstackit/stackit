package absorb

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

func TestGeneratePlanJSON(t *testing.T) {
	t.Parallel()
	t.Run("generates valid JSON with absorbed and unabsorbable hunks", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch-a": "main",
				"branch-b": "branch-a",
			})

		// Create commits on branches
		s.Checkout("branch-a")
		s.Scene.Repo.CreateChangeAndCommit("branch-a commit", "file-a")

		s.Checkout("branch-b")
		s.Scene.Repo.CreateChangeAndCommit("branch-b commit", "file-b")

		// Get commit SHA for branch-a
		commits, err := s.Engine.GetBranch("branch-a").GetAllCommits(engine.CommitFormatSHA)
		require.NoError(t, err)
		require.NotEmpty(t, commits)
		commitSHA := commits[0]

		// Create test data
		hunkTargets := []git.HunkTarget{
			{
				Hunk: git.Hunk{
					File:     "file-a",
					NewStart: 1,
					NewCount: 10,
					Content:  "+func hello() {\n+  return\n+}",
				},
				CommitSHA: commitSHA,
			},
		}

		unabsorbedHunks := []git.Hunk{
			{
				File:     "utils.go",
				NewStart: 1,
				NewCount: 5,
				Content:  "+func helper() string {\n+  return \"help\"\n+}",
			},
		}

		newFiles := []string{"new_file.go"}

		// Generate plan JSON
		planJSON, err := GeneratePlanJSON(
			"branch-b",
			hunkTargets,
			unabsorbedHunks,
			newFiles,
			s.Engine,
		)
		require.NoError(t, err)
		require.NotEmpty(t, planJSON)

		// Parse and validate the JSON
		var plan PlanJSON
		err = json.Unmarshal(planJSON, &plan)
		require.NoError(t, err)

		// Validate structure
		assert.Equal(t, "branch-b", plan.CurrentBranch)
		assert.Len(t, plan.Absorbed, 1)
		assert.Len(t, plan.Unabsorbable, 1)
		assert.Equal(t, []string{"new_file.go"}, plan.NewFiles)

		// Validate absorbed hunk
		absorbed := plan.Absorbed[0]
		assert.Equal(t, "file-a", absorbed.File)
		assert.Equal(t, "1-10", absorbed.Lines)
		assert.Equal(t, "branch-a", absorbed.TargetBranch)
		assert.Contains(t, absorbed.Content, "+func hello()")

		// Validate unabsorbable hunk
		unabsorbable := plan.Unabsorbable[0]
		assert.Equal(t, "utils.go", unabsorbable.File)
		assert.Equal(t, "1-5", unabsorbable.Lines)
		assert.Equal(t, "commutes_with_all", unabsorbable.Reason)
		assert.Contains(t, unabsorbable.Content, "+func helper()")

		// Validate stack structure
		assert.NotEmpty(t, plan.Stack)
		// Should include trunk and branches
		var foundTrunk, foundCurrent bool
		for _, node := range plan.Stack {
			if node.IsTrunk {
				foundTrunk = true
			}
			if node.IsCurrent {
				foundCurrent = true
				assert.Equal(t, "branch-b", node.Name)
			}
		}
		assert.True(t, foundTrunk, "should include trunk in stack")
		assert.True(t, foundCurrent, "should mark current branch")
	})

	t.Run("handles empty inputs", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch-a": "main",
			})

		s.Checkout("branch-a")

		// Generate plan with empty inputs
		planJSON, err := GeneratePlanJSON(
			"branch-a",
			[]git.HunkTarget{},
			[]git.Hunk{},
			nil,
			s.Engine,
		)
		require.NoError(t, err)

		var plan PlanJSON
		err = json.Unmarshal(planJSON, &plan)
		require.NoError(t, err)

		assert.Equal(t, "branch-a", plan.CurrentBranch)
		assert.Empty(t, plan.Absorbed)
		assert.Empty(t, plan.Unabsorbable)
		assert.Nil(t, plan.NewFiles)
	})
}

func TestFormatLines(t *testing.T) {
	t.Parallel()
	tests := []struct {
		start    int
		count    int
		expected string
	}{
		{1, 1, "1"},
		{1, 0, "1"},
		{10, 5, "10-14"},
		{100, 10, "100-109"},
	}

	for _, tc := range tests {
		result := formatLines(tc.start, tc.count)
		assert.Equal(t, tc.expected, result, "formatLines(%d, %d)", tc.start, tc.count)
	}
}
