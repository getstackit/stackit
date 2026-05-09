package git_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"stackit.dev/stackit/internal/git"
	"stackit.dev/stackit/testhelpers"
)

func TestRebase(t *testing.T) {
	t.Run("rebases branch onto parent", func(t *testing.T) {
		scene := testhelpers.NewScene(t, testhelpers.InitialCommitSceneSetup)

		// Create branch1
		err := scene.Repo.CreateAndCheckoutBranch("branch1")
		require.NoError(t, err)
		err = scene.Repo.CreateChangeAndCommit("branch1 change", "b1")
		require.NoError(t, err)

		// Create branch2 on top of branch1
		err = scene.Repo.CreateAndCheckoutBranch("branch2")
		require.NoError(t, err)
		err = scene.Repo.CreateChangeAndCommit("branch2 change", "b2")
		require.NoError(t, err)

		// Get the base revision (branch1's initial commit)
		branch1Rev, err := scene.Repo.GetRef("branch1")
		require.NoError(t, err)

		// Add commit to main (parent moves forward)
		err = scene.Repo.CheckoutBranch("main")
		require.NoError(t, err)
		err = scene.Repo.CreateChangeAndCommit("main update", "main")
		require.NoError(t, err)

		// Rebase branch1 onto new main
		runner := git.NewRunner(nil)
		result, err := runner.Rebase(context.Background(), "branch1", "main", branch1Rev)
		require.NoError(t, err)
		require.Equal(t, git.RebaseDone, result.Result)

		// Verify branch1 is now based on new main
		err = scene.Repo.CheckoutBranch("branch1")
		require.NoError(t, err)
		// branch1 should have the new main commit in its history
		commits, err := scene.Repo.ListCurrentBranchCommitMessages()
		require.NoError(t, err)
		require.Contains(t, commits, "main update")
	})

	t.Run("handles rebase conflict", func(t *testing.T) {
		scene := testhelpers.NewScene(t, func(s *testhelpers.Scene) error {
			// Create initial file that will be modified to create conflict
			return s.Repo.CreateChangeAndCommit("initial content", "conflict")
		})

		// Get the fork point (main's current SHA before branching)
		forkPoint, err := scene.Repo.GetRef("main")
		require.NoError(t, err)

		// Create branch1 with change to the SAME file
		err = scene.Repo.CreateAndCheckoutBranch("branch1")
		require.NoError(t, err)
		err = scene.Repo.CreateChange("branch1 modification", "conflict", false)
		require.NoError(t, err)
		err = scene.Repo.CreateChangeAndCommit("branch1 change", "b1")
		require.NoError(t, err)

		// Create conflicting change in main (modifying the same file)
		err = scene.Repo.CheckoutBranch("main")
		require.NoError(t, err)
		err = scene.Repo.CreateChange("main conflicting modification", "conflict", false)
		require.NoError(t, err)
		err = scene.Repo.CreateChangeAndCommit("main conflicting change", "main")
		require.NoError(t, err)

		// Rebase should result in conflict (using fork point, not branch tip)
		runner := git.NewRunner(nil)
		result, err := runner.Rebase(context.Background(), "branch1", "main", forkPoint)
		require.NoError(t, err)
		require.Equal(t, git.RebaseConflict, result.Result)

		// Verify rebase is in progress
		require.True(t, runner.IsRebaseInProgress(context.Background()))
	})

	t.Run("auto-continues when rerere resolved conflict", func(t *testing.T) {
		scene := testhelpers.NewScene(t, func(s *testhelpers.Scene) error {
			return s.Repo.CreateChangeAndCommit("initial content", "conflict")
		})
		runner := git.NewRunner(nil)
		require.NoError(t, runner.SetConfig("rerere.enabled", "true"))
		require.NoError(t, runner.SetConfig("rerere.autoupdate", "true"))

		forkPoint, err := scene.Repo.GetRef("main")
		require.NoError(t, err)

		require.NoError(t, scene.Repo.CreateAndCheckoutBranch("branch1"))
		require.NoError(t, scene.Repo.CreateChange("branch modification", "conflict", false))
		require.NoError(t, scene.Repo.RunGitCommand("commit", "-m", "branch change"))

		require.NoError(t, scene.Repo.CheckoutBranch("main"))
		require.NoError(t, scene.Repo.CreateChange("main modification", "conflict", false))
		require.NoError(t, scene.Repo.RunGitCommand("commit", "-m", "main change"))

		result, err := runner.Rebase(context.Background(), "branch1", "main", forkPoint)
		require.NoError(t, err)
		require.Equal(t, git.RebaseConflict, result.Result)
		require.True(t, runner.IsRebaseInProgress(context.Background()))

		require.NoError(t, scene.Repo.ResolveMergeConflicts())
		require.NoError(t, scene.Repo.MarkMergeConflictsAsResolved())
		result, err = runner.RebaseContinue(context.Background())
		require.NoError(t, err)
		require.Equal(t, git.RebaseDone, result.Result)
		require.False(t, runner.IsRebaseInProgress(context.Background()))

		require.NoError(t, scene.Repo.CheckoutBranch("main"))
		require.NoError(t, scene.Repo.RunGitCommand("checkout", "-b", "branch2", forkPoint))
		require.NoError(t, scene.Repo.CreateChange("branch modification", "conflict", false))
		require.NoError(t, scene.Repo.RunGitCommand("commit", "-m", "branch change again"))

		result, err = runner.Rebase(context.Background(), "branch2", "main", forkPoint)
		require.NoError(t, err)
		require.Equal(t, git.RebaseDone, result.Result)
		require.Greater(t, result.RerereResolvedCount, 0)
		require.False(t, runner.IsRebaseInProgress(context.Background()))
	})

	// Without rerere.autoupdate, rerere applies resolutions to the working
	// tree but does not stage them, so GetUnmergedFiles still reports the
	// file as unmerged and auto-continue must bail out. rerere.EnsureEnabled
	// is responsible for ensuring autoupdate is set whenever rerere is on.
	t.Run("does not auto-continue when autoupdate is disabled", func(t *testing.T) {
		scene := testhelpers.NewScene(t, func(s *testhelpers.Scene) error {
			return s.Repo.CreateChangeAndCommit("initial content", "conflict")
		})
		runner := git.NewRunner(nil)
		require.NoError(t, runner.SetConfig("rerere.enabled", "true"))
		require.NoError(t, runner.SetConfig("rerere.autoupdate", "false"))

		forkPoint, err := scene.Repo.GetRef("main")
		require.NoError(t, err)

		require.NoError(t, scene.Repo.CreateAndCheckoutBranch("branch1"))
		require.NoError(t, scene.Repo.CreateChange("branch modification", "conflict", false))
		require.NoError(t, scene.Repo.RunGitCommand("commit", "-m", "branch change"))

		require.NoError(t, scene.Repo.CheckoutBranch("main"))
		require.NoError(t, scene.Repo.CreateChange("main modification", "conflict", false))
		require.NoError(t, scene.Repo.RunGitCommand("commit", "-m", "main change"))

		// Resolve once so rerere records the resolution.
		result, err := runner.Rebase(context.Background(), "branch1", "main", forkPoint)
		require.NoError(t, err)
		require.Equal(t, git.RebaseConflict, result.Result)
		require.NoError(t, scene.Repo.ResolveMergeConflicts())
		require.NoError(t, scene.Repo.MarkMergeConflictsAsResolved())
		result, err = runner.RebaseContinue(context.Background())
		require.NoError(t, err)
		require.Equal(t, git.RebaseDone, result.Result)

		// Replay on a fresh branch. rerere will replay the resolution into
		// the working tree but won't stage it, so we still surface conflict.
		require.NoError(t, scene.Repo.CheckoutBranch("main"))
		require.NoError(t, scene.Repo.RunGitCommand("checkout", "-b", "branch2", forkPoint))
		require.NoError(t, scene.Repo.CreateChange("branch modification", "conflict", false))
		require.NoError(t, scene.Repo.RunGitCommand("commit", "-m", "branch change again"))

		result, err = runner.Rebase(context.Background(), "branch2", "main", forkPoint)
		require.NoError(t, err)
		require.Equal(t, git.RebaseConflict, result.Result)
		require.Equal(t, 0, result.RerereResolvedCount)
		require.True(t, runner.IsRebaseInProgress(context.Background()))
	})
}

func TestIsRebaseInProgress(t *testing.T) {
	t.Run("returns false when no rebase", func(t *testing.T) {
		_ = testhelpers.NewScene(t, testhelpers.InitialCommitSceneSetup)

		runner := git.NewRunner(nil)
		require.False(t, runner.IsRebaseInProgress(context.Background()))
	})

	t.Run("returns true when rebase in progress", func(t *testing.T) {
		scene := testhelpers.NewScene(t, func(s *testhelpers.Scene) error {
			// Create initial file that will be modified to create conflict
			return s.Repo.CreateChangeAndCommit("initial content", "conflict")
		})

		// Get the fork point before branching
		forkPoint, err := scene.Repo.GetRef("main")
		require.NoError(t, err)

		// Create branch and conflict scenario
		err = scene.Repo.CreateAndCheckoutBranch("branch1")
		require.NoError(t, err)
		err = scene.Repo.CreateChange("branch1 change", "conflict", false)
		require.NoError(t, err)
		err = scene.Repo.CreateChangeAndCommit("branch1 change", "b1")
		require.NoError(t, err)

		err = scene.Repo.CheckoutBranch("main")
		require.NoError(t, err)
		err = scene.Repo.CreateChange("main conflicting", "conflict", false)
		require.NoError(t, err)
		err = scene.Repo.CreateChangeAndCommit("main conflicting", "main")
		require.NoError(t, err)

		// Start rebase (will conflict)
		runner := git.NewRunner(nil)
		_, err = runner.Rebase(context.Background(), "branch1", "main", forkPoint)
		require.NoError(t, err)

		// Rebase should be in progress
		require.True(t, runner.IsRebaseInProgress(context.Background()))
	})
}

func TestRebaseContinue(t *testing.T) {
	t.Run("continues rebase after resolving conflict", func(t *testing.T) {
		scene := testhelpers.NewScene(t, func(s *testhelpers.Scene) error {
			// Create initial file that will be modified to create conflict
			return s.Repo.CreateChangeAndCommit("initial content", "conflict")
		})

		// Get the fork point before branching
		forkPoint, err := scene.Repo.GetRef("main")
		require.NoError(t, err)

		// Create branch with conflict
		err = scene.Repo.CreateAndCheckoutBranch("branch1")
		require.NoError(t, err)
		err = scene.Repo.CreateChange("branch1 change", "conflict", false)
		require.NoError(t, err)
		err = scene.Repo.CreateChangeAndCommit("branch1 change", "b1")
		require.NoError(t, err)

		err = scene.Repo.CheckoutBranch("main")
		require.NoError(t, err)
		err = scene.Repo.CreateChange("main conflicting", "conflict", false)
		require.NoError(t, err)
		err = scene.Repo.CreateChangeAndCommit("main conflicting", "main")
		require.NoError(t, err)

		// Start rebase (will conflict)
		runner := git.NewRunner(nil)
		_, err = runner.Rebase(context.Background(), "branch1", "main", forkPoint)
		require.NoError(t, err)
		require.True(t, runner.IsRebaseInProgress(context.Background()))

		// Resolve conflict
		err = scene.Repo.ResolveMergeConflicts()
		require.NoError(t, err)
		err = scene.Repo.MarkMergeConflictsAsResolved()
		require.NoError(t, err)

		// Continue rebase
		result, err := runner.RebaseContinue(context.Background())
		require.NoError(t, err)
		require.Equal(t, git.RebaseDone, result.Result)

		// Rebase should be complete
		require.False(t, runner.IsRebaseInProgress(context.Background()))
	})
}

// RebaseContinue delegates to AutoContinueRerereRebase whenever a manual
// `rebase --continue` leaves the rebase in progress with no unmerged files,
// which happens when rerere has already resolved and staged the next commit's
// conflict. The test pre-seeds rerere for the second commit's conflict, stops
// the rebase on the first commit's conflict, resolves it by hand, and then
// relies on RebaseContinue to drive the rebase to completion through the
// rerere-resolved second commit.
func TestRebaseContinueAutoContinuesThroughRerereResolvedCommit(t *testing.T) {
	scene := testhelpers.NewScene(t, func(s *testhelpers.Scene) error {
		return s.Repo.CreateChangeAndCommit("v0", "fileA.txt")
	})
	runner := git.NewRunner(nil)
	require.NoError(t, runner.SetConfig("rerere.enabled", "true"))
	require.NoError(t, runner.SetConfig("rerere.autoupdate", "true"))

	fork0, err := scene.Repo.GetRef("main")
	require.NoError(t, err)

	// Main commit M1 conflicts with branch1's first commit (fileA). Main
	// commit M2 conflicts with branch1's second commit (fileB).
	require.NoError(t, scene.Repo.CreateChange("mA", "fileA.txt", false))
	require.NoError(t, scene.Repo.RunGitCommand("commit", "-am", "main A"))
	mainAfterM1, err := scene.Repo.GetRef("main")
	require.NoError(t, err)
	require.NoError(t, scene.Repo.CreateChangeAndCommit("mB", "fileB.txt"))

	// Pre-seed rerere with a resolution for the fileB conflict by running
	// the same conflict through a throwaway branch and resolving by hand.
	require.NoError(t, scene.Repo.RunGitCommand("checkout", "-b", "seed", mainAfterM1))
	require.NoError(t, scene.Repo.CreateChangeAndCommit("branchB", "fileB.txt"))
	seedResult, err := runner.Rebase(context.Background(), "seed", "main", mainAfterM1)
	require.NoError(t, err)
	require.Equal(t, git.RebaseConflict, seedResult.Result)
	require.NoError(t, scene.Repo.RunGitCommand("checkout", "--theirs", "fileB.txt_test.txt"))
	require.NoError(t, scene.Repo.RunGitCommand("add", "fileB.txt_test.txt"))
	require.NoError(t, scene.Repo.RunGitCommand("rerere"))
	seedResult, err = runner.RebaseContinue(context.Background())
	require.NoError(t, err)
	require.Equal(t, git.RebaseDone, seedResult.Result)

	// Build branch1 off the original fork point with two commits. B1 clashes
	// with M1 (needs manual resolution), B2 clashes with M2 but reuses the
	// resolution rerere recorded above.
	require.NoError(t, scene.Repo.CheckoutBranch("main"))
	require.NoError(t, scene.Repo.RunGitCommand("checkout", "-b", "branch1", fork0))
	require.NoError(t, scene.Repo.CreateChangeAndCommit("bA", "fileA.txt"))
	require.NoError(t, scene.Repo.CreateChangeAndCommit("branchB", "fileB.txt"))

	result, err := runner.Rebase(context.Background(), "branch1", "main", fork0)
	require.NoError(t, err)
	require.Equal(t, git.RebaseConflict, result.Result)
	require.True(t, runner.IsRebaseInProgress(context.Background()))

	// User resolves the B1/fileA conflict manually.
	require.NoError(t, scene.Repo.RunGitCommand("checkout", "--theirs", "fileA.txt_test.txt"))
	require.NoError(t, scene.Repo.RunGitCommand("add", "fileA.txt_test.txt"))

	// RebaseContinue should drive the remaining B2 cherry-pick through
	// rerere and finish the rebase cleanly.
	contResult, err := runner.RebaseContinue(context.Background())
	require.NoError(t, err)
	require.Equal(t, git.RebaseDone, contResult.Result)
	require.False(t, runner.IsRebaseInProgress(context.Background()))
}

func TestGetRebaseHead(t *testing.T) {
	t.Run("returns rebase head when rebase in progress", func(t *testing.T) {
		scene := testhelpers.NewScene(t, func(s *testhelpers.Scene) error {
			// Create initial file that will be modified to create conflict
			return s.Repo.CreateChangeAndCommit("initial content", "conflict")
		})

		// Get the fork point before branching
		forkPoint, err := scene.Repo.GetRef("main")
		require.NoError(t, err)

		// Create conflict scenario
		err = scene.Repo.CreateAndCheckoutBranch("branch1")
		require.NoError(t, err)
		err = scene.Repo.CreateChange("branch1 change", "conflict", false)
		require.NoError(t, err)
		err = scene.Repo.CreateChangeAndCommit("branch1 change", "b1")
		require.NoError(t, err)

		err = scene.Repo.CheckoutBranch("main")
		require.NoError(t, err)
		err = scene.Repo.CreateChange("main conflicting", "conflict", false)
		require.NoError(t, err)
		err = scene.Repo.CreateChangeAndCommit("main conflicting", "main")
		require.NoError(t, err)

		// Start rebase (will conflict)
		runner := git.NewRunner(nil)
		_, err = runner.Rebase(context.Background(), "branch1", "main", forkPoint)
		require.NoError(t, err)

		// Verify we're in a conflict state
		require.True(t, runner.IsRebaseInProgress(context.Background()))

		// Get rebase head
		rebaseHead, err := runner.GetRebaseHead()
		require.NoError(t, err)
		require.NotEmpty(t, rebaseHead)
	})
}
