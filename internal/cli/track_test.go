package cli_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"stackit.dev/stackit/testhelpers"
	"stackit.dev/stackit/testhelpers/scenario"
)

func TestTrackCommand(t *testing.T) {
	t.Parallel()

	t.Run("track with --parent flag tracks single branch", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, func(sc *testhelpers.Scene) error {
			// Create initial commit
			return sc.Repo.CreateChangeAndCommit("initial", "init")
		}).WithInProcess(true)

		// Create a tracked branch
		err := s.Scene.Repo.CreateChange("a content", "a", false)
		require.NoError(t, err)
		s.RunCli("create", "a", "-m", "Add a")

		// Create an untracked branch from a
		err = s.Scene.Repo.CheckoutBranch("a")
		require.NoError(t, err)
		err = s.Scene.Repo.CreateChange("b content", "b", false)
		require.NoError(t, err)
		err = s.Scene.Repo.CreateAndCheckoutBranch("b")
		require.NoError(t, err)
		err = s.Scene.Repo.RunGitCommand("commit", "-m", "Add b")
		require.NoError(t, err)

		// Track branch b with parent a
		output, err := s.RunCliAndGetOutput("track", "b", "--parent", "a")
		require.NoError(t, err, "track command failed: %s", output)

		// Verify branch is tracked (check via parent command)
		output, err = s.RunCliAndGetOutput("parent")
		require.NoError(t, err, "parent command failed: %s", output)
		require.Equal(t, "a", strings.TrimSpace(output))
	})

	t.Run("track with --parent flag using trunk", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, func(sc *testhelpers.Scene) error {
			// Create initial commit
			return sc.Repo.CreateChangeAndCommit("initial", "init")
		}).WithInProcess(true)

		// Create an untracked branch
		err := s.Scene.Repo.CreateChange("feature content", "feature", false)
		require.NoError(t, err)
		err = s.Scene.Repo.CreateAndCheckoutBranch("feature")
		require.NoError(t, err)
		err = s.Scene.Repo.RunGitCommand("commit", "-m", "Add feature")
		require.NoError(t, err)

		// Track branch with trunk as parent
		output, err := s.RunCliAndGetOutput("track", "feature", "--parent", "main")
		require.NoError(t, err, "track command failed: %s", output)

		// Verify branch is tracked (check via parent command)
		output, err = s.RunCliAndGetOutput("parent")
		require.NoError(t, err, "parent command failed: %s", output)
		require.Equal(t, "main", strings.TrimSpace(output))
	})

	t.Run("track with --parent fails when parent not tracked", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, func(sc *testhelpers.Scene) error {
			// Create initial commit
			return sc.Repo.CreateChangeAndCommit("initial", "init")
		}).WithInProcess(true)

		// Create an untracked branch
		err := s.Scene.Repo.CreateChange("feature content", "feature", false)
		require.NoError(t, err)
		err = s.Scene.Repo.CreateAndCheckoutBranch("feature")
		require.NoError(t, err)
		err = s.Scene.Repo.RunGitCommand("commit", "-m", "Add feature")
		require.NoError(t, err)

		// Create another untracked branch
		err = s.Scene.Repo.CheckoutBranch("main")
		require.NoError(t, err)
		err = s.Scene.Repo.CreateChange("other content", "other", false)
		require.NoError(t, err)
		err = s.Scene.Repo.CreateAndCheckoutBranch("other")
		require.NoError(t, err)
		err = s.Scene.Repo.RunGitCommand("commit", "-m", "Add other")
		require.NoError(t, err)

		// Try to track feature with untracked parent (should fail)
		output, err := s.RunCliAndGetOutput("track", "feature", "--parent", "other")
		if err == nil {
			t.Fatalf("track should have failed but didn't. Output: %s", output)
		}
		t.Logf("Track command output: %s", output)
		t.Logf("Track command error: %v", err)
		require.Error(t, err, "track should fail when parent is not tracked")
		require.Contains(t, output, "must be tracked", "error output: %s", output)
	})

	t.Run("track with --parent fails when branch doesn't exist", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, func(sc *testhelpers.Scene) error {
			// Create initial commit
			return sc.Repo.CreateChangeAndCommit("initial", "init")
		}).WithInProcess(true)

		// Try to track non-existent branch
		output, err := s.RunCliAndGetOutput("track", "nonexistent", "--parent", "main")
		require.Error(t, err, "track should fail when branch doesn't exist")
		require.Contains(t, output, "reference not found")
	})

	t.Run("track with --force finds most recent tracked ancestor", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, func(sc *testhelpers.Scene) error {
			// Create initial commit
			return sc.Repo.CreateChangeAndCommit("initial", "init")
		}).WithInProcess(true)

		// Create a stack: main -> a -> b
		err := s.Scene.Repo.CreateChange("a content", "a", false)
		require.NoError(t, err)
		s.RunCli("create", "a", "-m", "Add a")

		err = s.Scene.Repo.CreateChange("b content", "b", false)
		require.NoError(t, err)
		s.RunCli("create", "b", "-m", "Add b")

		// Create an untracked branch from b
		err = s.Scene.Repo.CreateChange("c content", "c", false)
		require.NoError(t, err)
		err = s.Scene.Repo.CreateAndCheckoutBranch("c")
		require.NoError(t, err)
		err = s.Scene.Repo.RunGitCommand("commit", "-m", "Add c")
		require.NoError(t, err)

		// Track branch c with --force (should find b as most recent ancestor)
		output, err := s.RunCliAndGetOutput("track", "c", "--force")
		require.NoError(t, err, "track command failed: %s", output)

		// Verify branch is tracked with b as parent
		output, err = s.RunCliAndGetOutput("parent")
		require.NoError(t, err, "parent command failed: %s", output)
		require.Equal(t, "b", strings.TrimSpace(output))
	})

	t.Run("track with --force falls back to trunk when no tracked ancestor", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, func(sc *testhelpers.Scene) error {
			// Create initial commit
			return sc.Repo.CreateChangeAndCommit("initial", "init")
		}).WithInProcess(true)

		// Create an untracked branch from main
		err := s.Scene.Repo.CreateChange("feature content", "feature", false)
		require.NoError(t, err)
		err = s.Scene.Repo.CreateAndCheckoutBranch("feature")
		require.NoError(t, err)
		err = s.Scene.Repo.RunGitCommand("commit", "-m", "Add feature")
		require.NoError(t, err)

		// Track branch with --force (should find trunk as parent)
		output, err := s.RunCliAndGetOutput("track", "feature", "--force")
		require.NoError(t, err, "track command failed: %s", output)

		// Verify branch is tracked with main as parent
		output, err = s.RunCliAndGetOutput("parent")
		require.NoError(t, err, "parent command failed: %s", output)
		require.Equal(t, "main", strings.TrimSpace(output))
	})

	t.Run("track defaults to current branch", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, func(sc *testhelpers.Scene) error {
			// Create initial commit
			return sc.Repo.CreateChangeAndCommit("initial", "init")
		}).WithInProcess(true)

		// Create a tracked branch
		err := s.Scene.Repo.CreateChange("a content", "a", false)
		require.NoError(t, err)
		s.RunCli("create", "a", "-m", "Add a")

		// Create an untracked branch and checkout
		err = s.Scene.Repo.CheckoutBranch("a")
		require.NoError(t, err)
		err = s.Scene.Repo.CreateChange("b content", "b", false)
		require.NoError(t, err)
		err = s.Scene.Repo.CreateAndCheckoutBranch("b")
		require.NoError(t, err)
		err = s.Scene.Repo.RunGitCommand("commit", "-m", "Add b")
		require.NoError(t, err)

		// Track current branch (b) with parent a
		output, err := s.RunCliAndGetOutput("track", "--parent", "a")
		require.NoError(t, err, "track command failed: %s", output)

		// Verify branch is tracked
		output, err = s.RunCliAndGetOutput("parent")
		require.NoError(t, err, "parent command failed: %s", output)
		require.Equal(t, "a", strings.TrimSpace(output))
	})

	t.Run("track fails when not on branch and no branch specified", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, func(sc *testhelpers.Scene) error {
			// Create initial commit
			return sc.Repo.CreateChangeAndCommit("initial", "init")
		}).WithInProcess(true)

		// Detach HEAD
		err := s.Scene.Repo.RunGitCommand("checkout", "HEAD~0")
		require.NoError(t, err)

		// Try to track without specifying branch (should fail)
		output, err := s.RunCliAndGetOutput("track", "--parent", "main")
		require.Error(t, err, "track should fail when not on branch")
		require.Contains(t, output, "not on a branch")
	})

	t.Run("track already tracked branch updates parent", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, func(sc *testhelpers.Scene) error {
			// Create initial commit
			return sc.Repo.CreateChangeAndCommit("initial", "init")
		}).WithInProcess(true)

		// Create a stack: main -> a -> b
		err := s.Scene.Repo.CreateChange("a content", "a", false)
		require.NoError(t, err)
		s.RunCli("create", "a", "-m", "Add a")

		err = s.Scene.Repo.CreateChange("b content", "b", false)
		require.NoError(t, err)
		s.RunCli("create", "b", "-m", "Add b")

		// Create another branch c from b
		err = s.Scene.Repo.CheckoutBranch("b")
		require.NoError(t, err)
		err = s.Scene.Repo.CreateChange("c content", "c", false)
		require.NoError(t, err)
		err = s.Scene.Repo.CreateAndCheckoutBranch("c")
		require.NoError(t, err)
		err = s.Scene.Repo.RunGitCommand("commit", "-m", "Add c")
		require.NoError(t, err)

		// Track c with parent a (a is an ancestor of b, and b is an ancestor of c, so a is an ancestor of c)
		output, err := s.RunCliAndGetOutput("track", "c", "--parent", "a")
		require.NoError(t, err, "track command failed: %s", output)

		// Verify c has a as parent
		output, err = s.RunCliAndGetOutput("parent")
		require.NoError(t, err, "parent command failed: %s", output)
		require.Equal(t, "a", strings.TrimSpace(output))

		// Re-track c with parent b (should update parent)
		output, err = s.RunCliAndGetOutput("track", "c", "--parent", "b")
		require.NoError(t, err, "track command failed: %s", output)

		// Verify c now has b as parent
		output, err = s.RunCliAndGetOutput("parent")
		require.NoError(t, err, "parent command failed: %s", output)
		require.Equal(t, "b", strings.TrimSpace(output))
	})

	t.Run("track can fix corrupted metadata", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, func(sc *testhelpers.Scene) error {
			// Create initial commit
			return sc.Repo.CreateChangeAndCommit("initial", "init")
		}).WithInProcess(true)

		// Create a tracked branch
		err := s.Scene.Repo.CreateChange("a content", "a", false)
		require.NoError(t, err)
		s.RunCli("create", "a", "-m", "Add a")

		// Create another branch from a and track it
		err = s.Scene.Repo.CheckoutBranch("a")
		require.NoError(t, err)
		err = s.Scene.Repo.CreateChange("b content", "b", false)
		require.NoError(t, err)
		err = s.Scene.Repo.CreateAndCheckoutBranch("b")
		require.NoError(t, err)
		err = s.Scene.Repo.RunGitCommand("commit", "-m", "Add b")
		require.NoError(t, err)

		s.RunCli("track", "b", "--parent", "a")

		// Corrupt metadata by deleting the metadata ref
		err = s.Scene.Repo.RunGitCommand("update-ref", "-d", "refs/stackit/metadata/b")
		require.NoError(t, err)

		// Verify b is no longer tracked
		output, err := s.RunCliAndGetOutput("parent")
		require.NoError(t, err)
		require.Contains(t, output, "no parent")

		// Re-track b to fix corrupted metadata
		output, err = s.RunCliAndGetOutput("track", "b", "--parent", "a")
		require.NoError(t, err, "track command failed: %s", output)

		// Verify b is tracked again
		output, err = s.RunCliAndGetOutput("parent")
		require.NoError(t, err, "parent command failed: %s", output)
		require.Equal(t, "a", strings.TrimSpace(output))
	})
}
