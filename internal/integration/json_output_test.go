package integration

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"stackit.dev/stackit/internal/actions"
	"stackit.dev/stackit/internal/actions/sync"
	"stackit.dev/stackit/internal/handlers"
)

// =============================================================================
// JSON Output Integration Tests
//
// These tests cover JSON output functionality for various commands.
// =============================================================================

func TestJSONOutput(t *testing.T) {
	t.Parallel()

	t.Run("log --json outputs valid JSON with branch info", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		// Create a simple stack
		sh.Write("feature_a", "content a").
			Run("create feature-a -m 'Add feature A'").
			OnBranch("feature-a")

		sh.Write("feature_b", "content b").
			Run("create feature-b -m 'Add feature B'").
			OnBranch("feature-b")

		// Get JSON output
		sh.Run("log --json")
		output := sh.Output()

		// Parse and verify JSON structure
		var result actions.LogJSONResult
		err := json.Unmarshal([]byte(output), &result)
		require.NoError(t, err, "log --json should produce valid JSON")

		// Verify branches are present
		require.GreaterOrEqual(t, len(result.Branches), 2, "should have at least 2 branches")

		// Find feature-b (current branch)
		var foundFeatureB bool
		for _, b := range result.Branches {
			if b.Name == "feature-b" {
				foundFeatureB = true
				require.True(t, b.IsCurrent, "feature-b should be current")
				require.Equal(t, "feature-a", b.Parent, "feature-b parent should be feature-a")
				require.False(t, b.IsTrunk, "feature-b should not be trunk")
			}
		}
		require.True(t, foundFeatureB, "feature-b should be in output")

		// Verify summary
		require.Equal(t, 2, result.Summary.TotalBranches, "should have 2 tracked branches")
	})

	t.Run("log --json outputs valid JSON with recommendations", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		// Create branches that need attention
		sh.Write("feature_a", "content a").
			Run("create feature-a -m 'Add feature A'").
			OnBranch("feature-a")

		sh.Write("feature_b", "content b").
			Run("create feature-b -m 'Add feature B'").
			OnBranch("feature-b")

		// Modify parent to make child need restack
		sh.Checkout("feature-a").
			Commit("extra", "extra content")

		// Get JSON output
		sh.Run("log --json")
		output := sh.Output()

		// Parse and verify JSON structure
		var result actions.LogJSONResult
		err := json.Unmarshal([]byte(output), &result)
		require.NoError(t, err, "log --json should produce valid JSON")

		// Verify branches are present
		require.GreaterOrEqual(t, len(result.Branches), 2, "should have at least 2 branches")

		// Should show that feature-b needs restack
		foundRestackNeeded := false
		for _, branch := range result.Branches {
			if branch.Name == "feature-b" && branch.NeedsRestack {
				foundRestackNeeded = true
				break
			}
		}
		require.True(t, foundRestackNeeded, "feature-b should show NeedsRestack=true")
	})

	t.Run("log --quiet outputs minimal when healthy", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		// Create a healthy stack
		sh.Write("feature_a", "content a").
			Run("create feature-a -m 'Add feature A'")

		// Get output with --quiet
		sh.Run("log --quiet")
		output := sh.Output()

		// Should be empty or minimal when healthy
		// (may have some output if there are recommendations like "submit")
		require.NotContains(t, output, "needs restack", "healthy stack should not show restack needed")
	})

	t.Run("restack --json outputs valid JSON", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		// Create a stack
		sh.Write("feature_a", "content a").
			Run("create feature-a -m 'Add feature A'").
			OnBranch("feature-a")

		sh.Write("feature_b", "content b").
			Run("create feature-b -m 'Add feature B'").
			OnBranch("feature-b")

		// Get JSON output from restack (should report already up to date)
		sh.Run("restack --json")
		output := sh.Output()

		// Parse and verify JSON structure
		var result handlers.RestackJSONResult
		err := json.Unmarshal([]byte(output), &result)
		require.NoError(t, err, "restack --json should produce valid JSON")

		require.Equal(t, "success", result.Status, "status should be success")
		require.Empty(t, result.Conflicts, "should have no conflicts")
	})

	t.Run("restack --json reports branches that needed restacking", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		// Create a stack
		sh.Write("feature_a", "content a").
			Run("create feature-a -m 'Add feature A'").
			OnBranch("feature-a")

		sh.Write("feature_b", "content b").
			Run("create feature-b -m 'Add feature B'").
			OnBranch("feature-b")

		// Modify parent to make child need restack
		sh.Checkout("feature-a").
			Commit("extra", "extra content")

		// Restack with JSON output
		sh.Run("restack --json")
		output := sh.Output()

		// Parse and verify JSON structure
		var result handlers.RestackJSONResult
		err := json.Unmarshal([]byte(output), &result)
		require.NoError(t, err, "restack --json should produce valid JSON")

		require.Equal(t, "success", result.Status, "status should be success")
		// Either restacked or skipped, depending on whether it needed work
		require.GreaterOrEqual(t, result.TotalCount, 1, "should have processed at least 1 branch")
	})

	t.Run("sync --dry-run --json requires --dry-run", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		// Try to use --json without --dry-run
		sh.RunExpectError("sync --json").
			OutputContains("--json requires --dry-run")
	})

	t.Run("sync --dry-run --json outputs valid JSON", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t, WithRemote())

		// Create a stack
		sh.Write("feature_a", "content a").
			Run("create feature-a -m 'Add feature A'").
			OnBranch("feature-a")

		// Get JSON output from sync --dry-run
		sh.Run("sync --dry-run --json")
		output := sh.Output()

		// Parse and verify JSON structure
		var result sync.DryRunResult
		err := json.Unmarshal([]byte(output), &result)
		require.NoError(t, err, "sync --dry-run --json should produce valid JSON")

		// JSON was valid and parseable - verified by NoError above
	})

	t.Run("log --json with no tracked branches outputs valid JSON", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		// Don't create any tracked branches - just run log on a fresh repo
		sh.Run("log --json")
		output := sh.Output()

		// Parse and verify JSON structure
		var result actions.LogJSONResult
		err := json.Unmarshal([]byte(output), &result)
		require.NoError(t, err, "log --json should produce valid JSON even with no tracked branches")

		// With no tracked branches, should have trunk at minimum
		require.NotNil(t, result.Branches, "branches should not be nil")
		require.Equal(t, 0, result.Summary.TotalBranches, "summary total should be 0 with no tracked branches")
	})

	t.Run("log --json with mixed branch states", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		// Create multiple branches with different states
		sh.Write("feature_a", "content a").
			Run("create feature-a -m 'Add feature A'").
			OnBranch("feature-a")

		sh.Write("feature_b", "content b").
			Run("create feature-b -m 'Add feature B'").
			OnBranch("feature-b")

		// Lock one branch
		sh.Checkout("feature-a").
			Run("lock")

		// Create another branch that needs restack
		sh.Checkout("main").
			Write("feature_c", "content c").
			Run("create feature-c -m 'Add feature C'")

		// Modify main to make feature-c need restack
		sh.Checkout("main").
			Commit("main-update", "updating main")

		// Get JSON output
		sh.Run("log --json")
		output := sh.Output()

		var result actions.LogJSONResult
		err := json.Unmarshal([]byte(output), &result)
		require.NoError(t, err)

		// Verify we have multiple branches with different states
		require.GreaterOrEqual(t, len(result.Branches), 3)

		// Find the locked branch
		var foundLocked bool
		for _, b := range result.Branches {
			if b.Name == "feature-a" {
				require.True(t, b.IsLocked, "feature-a should be locked")
				foundLocked = true
			}
		}
		require.True(t, foundLocked, "should find locked branch")
	})

	t.Run("log --json includes children relationships", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		// Create a branching stack structure
		// main -> feature-a -> feature-a-1
		//                   -> feature-a-2
		sh.Write("feature_a", "content a").
			Run("create feature-a -m 'Add feature A'")

		sh.Write("feature_a_1", "content a1").
			Run("create feature-a-1 -m 'Add feature A1'")

		sh.Checkout("feature-a").
			Write("feature_a_2", "content a2").
			Run("create feature-a-2 -m 'Add feature A2'")

		// Get JSON output
		sh.Run("log --json")
		output := sh.Output()

		var result actions.LogJSONResult
		err := json.Unmarshal([]byte(output), &result)
		require.NoError(t, err)

		// Find feature-a and verify it has children
		for _, b := range result.Branches {
			if b.Name == "feature-a" {
				require.Len(t, b.Children, 2, "feature-a should have 2 children")
				require.Contains(t, b.Children, "feature-a-1")
				require.Contains(t, b.Children, "feature-a-2")
			}
		}
	})

	t.Run("log --json reports branches without GitHub available", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		// Create branches (no GitHub configured in test environment)
		sh.Write("feature_a", "content a").
			Run("create feature-a -m 'Add feature A'")

		// Get JSON output
		sh.Run("log --json")
		output := sh.Output()

		var result actions.LogJSONResult
		err := json.Unmarshal([]byte(output), &result)
		require.NoError(t, err)

		// GitHubAvailable should be false in test environment
		require.False(t, result.GitHubAvailable)

		// CI status should be empty for all branches without PRs
		for _, b := range result.Branches {
			if b.PR != nil {
				require.Equal(t, "", b.PR.CIStatus, "CI should be empty without GitHub")
			}
		}
	})
}
