package navigation_test

import (
	"encoding/json"
	"maps"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

func TestLogCommand(t *testing.T) {
	t.Parallel()

	t.Run("tree in empty repo", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenarioParallel(t, testhelpers.BasicSceneSetup).WithInProcess(true)

		// Run tree command
		output, err := s.RunCliAndGetOutput("tree")

		// Should succeed and show trunk branch
		require.NoError(t, err, "tree command failed: %s", output)
		require.Contains(t, output, "main")
	})

	t.Run("tree with branches", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenarioParallel(t, testhelpers.BasicSceneSetup).WithInProcess(true)

		// Create a branch
		s.CreateBranch("feature").
			CommitChange("feature", "feature commit")

		// Checkout main
		s.Checkout("main")

		// Run tree command with --show-untracked to see untracked branches
		output, err := s.RunCliAndGetOutput("tree", "--show-untracked")

		require.NoError(t, err, "tree command failed: %s", output)
		require.Contains(t, output, "main")
		require.Contains(t, output, "feature")
	})

	t.Run("tree with --stack flag", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenarioParallel(t, testhelpers.BasicSceneSetup).WithInProcess(true)

		// Create and checkout a branch
		s.CreateBranch("feature")

		// Run tree command with stack
		output, err := s.RunCliAndGetOutput("tree", "--stack")

		require.NoError(t, err, "tree command failed: %s", output)
		require.Contains(t, output, "feature")
	})

	t.Run("t aliases tree short", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenarioParallel(t, testhelpers.BasicSceneSetup).WithInProcess(true)
		s.CreateBranch("feature")

		shortOutput, err := s.RunCliAndGetOutput("tree", "short")
		require.NoError(t, err, "tree short command failed: %s", shortOutput)

		aliasOutput, err := s.RunCliAndGetOutput("t")
		require.NoError(t, err, "t command failed: %s", aliasOutput)

		require.Equal(t, shortOutput, aliasOutput)
		require.Contains(t, aliasOutput, "main")
	})

	t.Run("tree shows worktree indicator for stack with worktree", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenarioParallel(t, testhelpers.BasicSceneSetup).WithInProcess(true)
		s.WithInitialCommit()

		// Create a staged change for the branch
		s.CommitChange("feature-file", "feature content")

		// Go back to main to create branch with worktree
		s.Checkout("main")

		// Stage a change for the worktree branch
		err := s.Scene.Repo.CreateChange("worktree-content", "worktree-file", false)
		require.NoError(t, err)

		// Create branch with worktree using CLI
		output, err := s.RunCliAndGetOutput("create", "-m", "worktree feature", "-w")
		require.NoError(t, err, "create with worktree failed: %s", output)

		// Run tree command - should show worktree indicator
		output, err = s.RunCliAndGetOutput("tree")
		require.NoError(t, err, "tree command failed: %s", output)
		require.Contains(t, output, "worktree", "tree should show worktree indicator for branch with managed worktree")
	})
}

// treeJSONBranches runs a tree JSON command and returns the branches keyed by
// name, asserting that no branch names a child absent from the result.
func treeJSONBranches(t *testing.T, s *scenario.Scenario, args ...string) map[string]map[string]any {
	t.Helper()
	output, err := s.RunCliAndGetOutput(args...)
	require.NoError(t, err, output)
	var result struct {
		Branches []map[string]any `json:"branches"`
	}
	require.NoError(t, json.Unmarshal([]byte(output), &result))
	byName := make(map[string]map[string]any, len(result.Branches))
	for _, branch := range result.Branches {
		byName[branch["name"].(string)] = branch
	}
	for name, branch := range byName {
		children, _ := branch["children"].([]any)
		for _, child := range children {
			require.Contains(t, byName, child, "%s names child %v outside the result", name, child)
		}
	}
	return byName
}

func TestTreeShortJSONRespectsSteps(t *testing.T) {
	t.Parallel()
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).WithLinearStack3().WithInProcess(true)
	s.Checkout("a")
	for _, command := range [][]string{{"tree", "short"}, {"t"}} {
		branches := treeJSONBranches(t, s, append(command, "--steps", "1", "--json")...)
		require.Contains(t, branches, "b")
		require.NotContains(t, branches, "c", "--steps 1 from a must not reach c")
		for _, branch := range branches {
			require.NotContains(t, branch, "commits")
			require.NotContains(t, branch, "pr")
		}
	}
}

func TestTreeStackJSONMatchesTextScope(t *testing.T) {
	t.Parallel()
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).WithLinearStack3().WithInProcess(true)
	s.Checkout("main").CreateBranch("other").Commit("other").TrackBranch("other", "main")
	s.Checkout("b")

	branches := treeJSONBranches(t, s, "tree", "--stack", "--json")
	require.ElementsMatch(t, []string{"main", "a", "b", "c"}, slices.Collect(maps.Keys(branches)),
		"--stack JSON draws the same branches as the text view, trunk included")
	require.Equal(t, []any{"a"}, branches["main"]["children"], "the unrelated stack is out of scope")
}

func TestTreeNormalJSONRespectsSteps(t *testing.T) {
	t.Parallel()
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).WithLinearStack3().WithInProcess(true)
	s.Checkout("a")
	output, err := s.RunCliAndGetOutput("tree", "--steps", "1", "--json")
	require.NoError(t, err, output)
	var result struct {
		Branches        []map[string]any `json:"branches"`
		GitHubAvailable *bool            `json:"github_available"`
	}
	require.NoError(t, json.Unmarshal([]byte(output), &result))
	names := make([]string, 0, len(result.Branches))
	for _, branch := range result.Branches {
		names = append(names, branch["name"].(string))
		require.Contains(t, branch, "commits", "normal JSON keeps commit counts")
	}
	require.NotContains(t, names, "c", "--steps 1 from a must not reach c")
	require.Contains(t, names, "b")
	require.NotNil(t, result.GitHubAvailable, "normal JSON keeps github_available")
}
