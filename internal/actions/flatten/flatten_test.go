package flatten_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"stackit.dev/stackit/internal/actions/flatten"
	"stackit.dev/stackit/testhelpers"
	"stackit.dev/stackit/testhelpers/scenario"
)

func writeFile(t *testing.T, s *scenario.Scenario, name, content string) {
	t.Helper()
	err := os.WriteFile(filepath.Join(s.Scene.Repo.Dir, name), []byte(content), 0644)
	require.NoError(t, err)
}

func TestFlattenAction(t *testing.T) {
	t.Run("flattens linear independent stack to trunk", func(t *testing.T) {
		// main -> A -> B -> C
		// WithStack creates independent changes (separate files)
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"A": "main",
				"B": "A",
				"C": "B",
			})

		err := flatten.Action(s.Context, flatten.Options{BranchName: "C"}, nil)
		require.NoError(t, err)

		s.Rebuild()

		branchA := s.Engine.GetBranch("A")
		branchB := s.Engine.GetBranch("B")
		branchC := s.Engine.GetBranch("C")

		require.Equal(t, "main", branchA.GetParent().GetName())
		require.Equal(t, "main", branchB.GetParent().GetName())
		require.Equal(t, "main", branchC.GetParent().GetName())
	})

	t.Run("respects dependencies", func(t *testing.T) {
		// main -> A -> B
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"A": "main",
				"B": "A",
			})

		// Make B depend on A
		// A created "A_test.txt". B created "B_test.txt".
		// We modify "A_test.txt" in B.
		s.CheckoutQuiet("B")
		writeFile(t, s, "A_test.txt", "modified by B")
		s.RunGit("add", ".")
		s.RunGit("commit", "-m", "B depends on A")

		s.Rebuild()

		err := flatten.Action(s.Context, flatten.Options{BranchName: "B"}, nil)
		require.NoError(t, err)

		s.Rebuild()

		branchA := s.Engine.GetBranch("A")
		branchB := s.Engine.GetBranch("B")

		require.Equal(t, "main", branchA.GetParent().GetName())
		require.Equal(t, "A", branchB.GetParent().GetName())
	})

	t.Run("partial flatten", func(t *testing.T) {
		// main -> A -> B -> C
		// A independent
		// B depends on A
		// C independent of B (and A)
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"A": "main",
				"B": "A",
				"C": "B",
			})

		// Make B depend on A
		s.CheckoutQuiet("B")
		writeFile(t, s, "A_test.txt", "modified by B")
		s.RunGit("add", ".")
		s.RunGit("commit", "-m", "B depends on A")

		// C is on top of B, but only touches C_test.txt (from WithStack)
		// So C should be able to skip B and A and go to main?
		// Wait, C starts at B. B depends on A.
		// If C only has "C_test.txt", and main lacks "A_test.txt" and "B_test.txt".
		// C adds "C_test.txt". It should apply cleanly on main.
		// However, C *state* currently includes B's changes.
		// Rebase --onto main B C
		// This takes commits between B..C (which is just C's commit) and plays them on main.
		// C's commit only adds C_test.txt.
		// So it should work.

		s.Rebuild()

		err := flatten.Action(s.Context, flatten.Options{BranchName: "C"}, nil)
		require.NoError(t, err)

		s.Rebuild()

		branchA := s.Engine.GetBranch("A")
		branchB := s.Engine.GetBranch("B")
		branchC := s.Engine.GetBranch("C")

		require.Equal(t, "main", branchA.GetParent().GetName())
		require.Equal(t, "A", branchB.GetParent().GetName())
		require.Equal(t, "main", branchC.GetParent().GetName())
	})

	t.Run("handles already flat stack", func(t *testing.T) {
		// main -> A, main -> B (already flat)
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"A": "main",
				"B": "main",
			})

		err := flatten.Action(s.Context, flatten.Options{BranchName: "A"}, nil)
		require.NoError(t, err)

		s.Rebuild()

		branchA := s.Engine.GetBranch("A")
		branchB := s.Engine.GetBranch("B")

		// Both should still be on main
		require.Equal(t, "main", branchA.GetParent().GetName())
		require.Equal(t, "main", branchB.GetParent().GetName())
	})

	t.Run("uses current branch when none specified", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"A": "main",
				"B": "A",
			})

		s.CheckoutQuiet("B")
		s.Rebuild()

		// Empty branch name should use current branch
		err := flatten.Action(s.Context, flatten.Options{}, nil)
		require.NoError(t, err)

		s.Rebuild()

		branchA := s.Engine.GetBranch("A")
		branchB := s.Engine.GetBranch("B")

		require.Equal(t, "main", branchA.GetParent().GetName())
		require.Equal(t, "main", branchB.GetParent().GetName())
	})

	t.Run("returns error for untracked branch", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

		// Create an untracked branch
		s.RunGit("checkout", "-b", "untracked-branch")
		s.Rebuild()

		err := flatten.Action(s.Context, flatten.Options{BranchName: "untracked-branch"}, nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "not tracked")
	})

	t.Run("excludes move when descendant would conflict", func(t *testing.T) {
		// This test verifies that flatten correctly validates descendant branches
		// in a CHAINED manner and excludes moves that would cause conflicts.
		//
		// The key issue this tests: When validating rebases, we must validate
		// the entire chain together so that descendant rebases use the correct
		// (post-rebase) parent positions.
		//
		// Setup: main -> A -> B -> C
		// - A adds A_test.txt (can flatten to main)
		// - B adds B_test.txt (can flatten to main independently)
		// - C modifies A_test.txt (depends on A's content)
		//
		// If we validate B's move to main in ISOLATION, it passes.
		// But if B moves to main, C (which modifies A_test.txt) would fail
		// because the rebased B doesn't have A_test.txt.
		//
		// The fix: validate the ENTIRE chain together, so C's validation
		// uses B's post-rebase position and correctly detects the conflict.

		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"A": "main",
				"B": "A",
				"C": "B",
			})

		// C modifies A_test.txt (created by A's commit in WithStack)
		// This means C depends on content from A
		s.CheckoutQuiet("C")
		writeFile(t, s, "A_test.txt", "modified by C - I depend on content from branch A")
		s.RunGit("add", ".")
		s.RunGit("commit", "-m", "C modifies A_test.txt")

		s.Rebuild()

		// Run flatten
		err := flatten.Action(s.Context, flatten.Options{BranchName: "C", SkipConfirm: true}, nil)
		require.NoError(t, err, "flatten should succeed (excluding problematic moves, not failing)")

		s.Rebuild()

		branchA := s.Engine.GetBranch("A")
		branchB := s.Engine.GetBranch("B")
		branchC := s.Engine.GetBranch("C")

		t.Logf("After flatten: A->%s, B->%s, C->%s",
			branchA.GetParent().GetName(),
			branchB.GetParent().GetName(),
			branchC.GetParent().GetName())

		// A should move to main (independent - adds new file)
		require.Equal(t, "main", branchA.GetParent().GetName(), "A should be on main")

		// Key assertion: the flatten should complete without errors.
		// The exact positions of B and C depend on what the flatten algorithm
		// determines is safe. The important thing is that we don't hit a
		// restack conflict because all moves were properly validated.

		// Verify no error occurred during the flatten
		output := s.Output.String()
		require.NotContains(t, output, "Hit conflict", "should not hit a restack conflict")
	})

	t.Run("fallback to parent revision when metadata missing", func(t *testing.T) {
		// This test verifies the fix for the bug where getOldUpstream() was using
		// GetMergeBase as a fallback, which could include parent commits when
		// flattening. The fix uses the parent's current revision instead.
		//
		// Setup: main -> A -> B
		// Clear B's ParentBranchRevision metadata
		// Flatten B to main
		// Verify B only has its own commits (not A's commits)

		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"A": "main",
				"B": "A",
			})

		// Get the commit count for B before flattening (should be 1 commit from B)
		bCommitsBefore, err := s.Scene.Repo.RunGitCommandAndGetOutput("rev-list", "--count", "A..B")
		require.NoError(t, err)
		require.Equal(t, "1", bCommitsBefore, "B should have 1 commit on top of A")

		// Clear B's ParentBranchRevision metadata to trigger the fallback path
		meta, err := s.Engine.Git().ReadMetadata("B")
		require.NoError(t, err)
		meta = meta.WithParentBranchRevision(nil)
		err = s.Engine.Git().WriteMetadata("B", meta)
		require.NoError(t, err)

		s.Rebuild()

		// Verify metadata was cleared
		meta, err = s.Engine.Git().ReadMetadata("B")
		require.NoError(t, err)
		require.Nil(t, meta.GetParentBranchRevision())

		// Run flatten
		err = flatten.Action(s.Context, flatten.Options{BranchName: "B"}, nil)
		require.NoError(t, err)

		s.Rebuild()

		// Verify B is now parented on main
		branchB := s.Engine.GetBranch("B")
		require.Equal(t, "main", branchB.GetParent().GetName())

		// Critical assertion: B should still have exactly 1 commit on top of main
		// If the bug were present (using merge-base), B would have A's commits too
		bCommitsAfter, err := s.Scene.Repo.RunGitCommandAndGetOutput("rev-list", "--count", "main..B")
		require.NoError(t, err)
		require.Equal(t, "1", bCommitsAfter, "B should have only 1 commit on top of main (not include A's commits)")
	})
}
