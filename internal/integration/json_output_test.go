package integration

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/actions"
	"github.com/getstackit/stackit/internal/actions/sync"
	"github.com/getstackit/stackit/internal/handlers"
)

// =============================================================================
// JSON Output Integration Tests
//
// These tests cover JSON output functionality for various commands.
// =============================================================================

func TestJSONOutput(t *testing.T) {
	t.Parallel()

	t.Run("tree --json outputs valid JSON with branch info", func(t *testing.T) {
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
		sh.Run("tree --json")
		output := sh.Output()

		// Parse and verify JSON structure
		var result actions.TreeJSONResult
		err := json.Unmarshal([]byte(output), &result)
		require.NoError(t, err, "tree --json should produce valid JSON")

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

		// GitHub is never available in tests.
		require.False(t, result.GitHubAvailable)
	})

	t.Run("tree --json outputs valid JSON with recommendations", func(t *testing.T) {
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
		sh.Run("tree --json")
		output := sh.Output()

		// Parse and verify JSON structure
		var result actions.TreeJSONResult
		err := json.Unmarshal([]byte(output), &result)
		require.NoError(t, err, "tree --json should produce valid JSON")

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

	t.Run("tree --quiet outputs minimal when healthy", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		// Create a healthy stack
		sh.Write("feature_a", "content a").
			Run("create feature-a -m 'Add feature A'")

		// Get output with --quiet
		sh.Run("tree --quiet")
		output := sh.Output()

		// Should be empty or minimal when healthy
		// (may have some output if there are recommendations like "submit")
		require.NotContains(t, output, "needs restack", "healthy stack should not show restack needed")
	})

	t.Run("restack --json outputs valid JSON", func(t *testing.T) {
		t.Parallel()
		for _, tt := range []struct {
			name          string
			advanceParent bool
			minTotal      int
		}{
			{"already up to date", false, 0},
			{"reports branches that needed restacking", true, 1},
		} {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				sh := NewTestShellInProcess(t)

				sh.Write("feature_a", "content a").
					Run("create feature-a -m 'Add feature A'").
					OnBranch("feature-a")

				sh.Write("feature_b", "content b").
					Run("create feature-b -m 'Add feature B'").
					OnBranch("feature-b")

				if tt.advanceParent {
					sh.Checkout("feature-a").Commit("extra", "extra content")
				}

				sh.Run("restack --json")
				output := sh.Output()

				var result handlers.RestackJSONResult
				err := json.Unmarshal([]byte(output), &result)
				require.NoError(t, err, "restack --json should produce valid JSON")
				require.Equal(t, handlers.RestackJSONStatusSuccess, result.Status)
				require.Empty(t, result.Conflicts)
				require.GreaterOrEqual(t, result.TotalCount, tt.minTotal)
			})
		}
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
		sh := NewTestShellInProcess(t)

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

	t.Run("sync --dry-run --json --restack reports stack roots for would_restack branches", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		// Build two independent stacks rooted at trunk:
		//   main -> alpha-root -> alpha-child
		//   main -> beta-root
		sh.Write("alpha_root", "alpha root").
			Run("create alpha-root -m 'Add alpha root'").
			Write("alpha_child", "alpha child").
			Run("create alpha-child -m 'Add alpha child'").
			Run("trunk").
			Write("beta_root", "beta root").
			Run("create beta-root -m 'Add beta root'")

		// Force alpha-root to diverge from trunk by amending trunk's view via a new commit on trunk.
		// We use a direct git call to shift trunk, then mark alpha-root as needing restack.
		sh.Run("checkout alpha-root").
			Git("commit --allow-empty -m 'force restack'")

		// Checkout back to alpha-child so restack scope discovery sees the stack.
		sh.Run("checkout alpha-child")

		sh.Run("sync --dry-run --json --restack")
		output := sh.Output()

		var result sync.DryRunResult
		err := json.Unmarshal([]byte(output), &result)
		require.NoError(t, err, "sync --dry-run --json --restack should produce valid JSON")

		// Every would_restack branch should map to a root; the deduped set should be
		// sorted and contain only known stack roots (alpha-root, beta-root).
		require.NotNil(t, result.WouldRestackStacks, "would_restack_stacks should be populated when branches need restack")
		require.Equal(t, []string{"alpha-root"}, result.WouldRestackStacks,
			"should deduplicate and sort restack roots; only alpha's stack has drift")
	})

	t.Run("tree --json with mixed branch states", func(t *testing.T) {
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
		sh.Run("tree --json")
		output := sh.Output()

		var result actions.TreeJSONResult
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

	t.Run("tree --json includes children relationships", func(t *testing.T) {
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
		sh.Run("tree --json")
		output := sh.Output()

		var result actions.TreeJSONResult
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
}

func TestStateJSON(t *testing.T) {
	t.Parallel()

	t.Run("produces a complete snapshot", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)
		sh.Write("feature_a", "content a").
			Run("create feature-a -m 'Add feature A'")

		sh.Run("state --json")
		var result actions.StateResult
		require.NoError(t, json.Unmarshal([]byte(sh.Output()), &result), "state --json should be valid JSON")

		require.Equal(t, "feature-a", result.CurrentBranch)
		require.False(t, result.Detached)
		require.NotEmpty(t, result.Trunk)
		// No in-progress operation on a fresh stack.
		require.Equal(t, "none", result.Operation.Kind)
		require.False(t, result.Operation.InProgress)
		require.NotNil(t, result.Operation.ConflictedFiles)
		// The staged file was committed by create, so the tree is clean.
		require.True(t, result.WorkingTree.Clean)
		// The stack is embedded (same shape as tree --json).
		require.GreaterOrEqual(t, result.Stack.Summary.TotalBranches, 1)
	})

	t.Run("reports a dirty working tree", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)
		sh.Write("feature_a", "content a").
			Run("create feature-a -m 'Add feature A'")
		sh.WriteFile("extra.txt", "x") // WriteFile stages the file

		sh.Run("state --json")
		var result actions.StateResult
		require.NoError(t, json.Unmarshal([]byte(sh.Output()), &result))

		require.False(t, result.WorkingTree.Clean)
		require.True(t, result.WorkingTree.Staged)
	})

	t.Run("human output summarizes branch and stack", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)
		sh.Write("feature_a", "content a").
			Run("create feature-a -m 'Add feature A'")

		sh.Run("state").
			OutputContains("On feature-a").
			OutputContains("Stack")
	})
}
