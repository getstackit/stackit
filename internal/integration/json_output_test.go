package integration

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/actions"
	"github.com/getstackit/stackit/internal/actions/state"
	"github.com/getstackit/stackit/internal/actions/sync"
	"github.com/getstackit/stackit/internal/cli/stack"
)

// =============================================================================
// JSON Output Integration Tests
//
// These tests cover JSON output functionality for various commands.
// =============================================================================

func TestJSONOutput(t *testing.T) {
	t.Parallel()

	// One feature-a/feature-b shell walks through the JSON surfaces in order:
	// read-only reports on a single branch, then a healthy two-branch stack,
	// then a stack whose child needs restacking, then a dirty working tree.
	t.Run("single-branch stack lifecycle", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		sh.Write("feature_a", "content a").
			Run("create feature-a -m 'Add feature A'").
			OnBranch("feature-a")

		t.Run("state, tree --quiet, and sync dry-run on one branch", func(t *testing.T) {
			sh := sh.WithT(t)
			requireReadOnly(sh, func() {
				sh.Run("state --json")
				var st state.Result
				require.NoError(t, json.Unmarshal([]byte(sh.Output()), &st), "state --json should be valid JSON")

				require.Equal(t, "feature-a", st.CurrentBranch)
				require.False(t, st.Detached)
				require.NotEmpty(t, st.Trunk)
				// No in-progress operation on a fresh stack.
				require.Equal(t, "none", st.Operation.Kind)
				require.False(t, st.Operation.InProgress)
				require.NotNil(t, st.Operation.ConflictedFiles)
				// The staged file was committed by create, so the tree is clean.
				require.True(t, st.WorkingTree.Clean)
				// The stack is embedded (same shape as tree --json).
				require.GreaterOrEqual(t, st.Stack.Summary.TotalBranches, 1)

				sh.Run("state").
					OutputContains("On feature-a").
					OutputContains("Stack")

				// Should be empty or minimal when healthy
				// (may have some output if there are recommendations like "submit")
				sh.Run("tree --quiet")
				require.NotContains(t, sh.Output(), "needs restack", "healthy stack should not show restack needed")

				// --json without --dry-run is rejected
				sh.RunExpectError("sync --json").
					OutputContains("--json requires --dry-run")

				sh.Run("sync --dry-run --json")
				var dryRun sync.DryRunResult
				require.NoError(t, json.Unmarshal([]byte(sh.Output()), &dryRun), "sync --dry-run --json should produce valid JSON")
			})
		})

		t.Run("tree --json and restack --json on a healthy two-branch stack", func(t *testing.T) {
			sh := sh.WithT(t)
			sh.Write("feature_b", "content b").
				Run("create feature-b -m 'Add feature B'").
				OnBranch("feature-b")

			sh.Run("tree --json")
			var result actions.TreeJSONResult
			require.NoError(t, json.Unmarshal([]byte(sh.Output()), &result), "tree --json should produce valid JSON")

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
			require.NotNil(t, result.GitHubAvailable)
			require.False(t, *result.GitHubAvailable)

			// Already up to date
			sh.Run("restack --json")
			var restack stack.RestackJSONResult
			require.NoError(t, json.Unmarshal([]byte(sh.Output()), &restack), "restack --json should produce valid JSON")
			require.Equal(t, stack.RestackJSONStatusSuccess, restack.Status)
			require.Empty(t, restack.Conflicts)
			require.GreaterOrEqual(t, restack.TotalCount, 0)
		})

		t.Run("tree --json and restack --json after the parent advances", func(t *testing.T) {
			sh := sh.WithT(t)
			// Modify parent to make child need restack
			sh.Checkout("feature-a").
				Commit("extra", "extra content")

			sh.Run("tree --json")
			var result actions.TreeJSONResult
			require.NoError(t, json.Unmarshal([]byte(sh.Output()), &result), "tree --json should produce valid JSON")

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

			// Reports branches that needed restacking
			sh.Run("restack --json")
			var restack stack.RestackJSONResult
			require.NoError(t, json.Unmarshal([]byte(sh.Output()), &restack), "restack --json should produce valid JSON")
			require.Equal(t, stack.RestackJSONStatusSuccess, restack.Status)
			require.Empty(t, restack.Conflicts)
			require.GreaterOrEqual(t, restack.TotalCount, 1)
		})

		t.Run("state --json reports a dirty working tree", func(t *testing.T) {
			sh := sh.WithT(t)
			sh.WriteFile("extra.txt", "x") // WriteFile stages the file

			sh.Run("state --json")
			var result state.Result
			require.NoError(t, json.Unmarshal([]byte(sh.Output()), &result))

			require.False(t, result.WorkingTree.Clean)
			require.True(t, result.WorkingTree.Staged)
		})
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

	// Children and mixed (locked + needs-restack) states share one shell: the
	// children check runs first, then the lock and trunk advance layer on top.
	t.Run("tree --json children and mixed branch states", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		// main -> feature-a -> feature-a-1
		//                   -> feature-a-2
		sh.Write("feature_a", "content a").
			Run("create feature-a -m 'Add feature A'")

		sh.Write("feature_a_1", "content a1").
			Run("create feature-a-1 -m 'Add feature A1'")

		sh.Checkout("feature-a").
			Write("feature_a_2", "content a2").
			Run("create feature-a-2 -m 'Add feature A2'")

		t.Run("includes children relationships", func(t *testing.T) {
			sh := sh.WithT(t)
			sh.Run("tree --json")
			var result actions.TreeJSONResult
			require.NoError(t, json.Unmarshal([]byte(sh.Output()), &result))

			// Find feature-a and verify it has children
			var foundFeatureA bool
			for _, b := range result.Branches {
				if b.Name == "feature-a" {
					foundFeatureA = true
					require.Len(t, b.Children, 2, "feature-a should have 2 children")
					require.Contains(t, b.Children, "feature-a-1")
					require.Contains(t, b.Children, "feature-a-2")
				}
			}
			require.True(t, foundFeatureA, "feature-a should be in output")
		})

		t.Run("with mixed branch states", func(t *testing.T) {
			sh := sh.WithT(t)
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

			sh.Run("tree --json")
			var result actions.TreeJSONResult
			require.NoError(t, json.Unmarshal([]byte(sh.Output()), &result))

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
	})
}
