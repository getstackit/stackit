package stack_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"stackit.dev/stackit/testhelpers"
)

func TestForeachCommand(t *testing.T) {
	t.Parallel()
	binaryPath := testhelpers.GetSharedBinaryPath()
	if binaryPath == "" {
		if err := testhelpers.GetBinaryError(); err != nil {
			t.Fatalf("failed to build stackit binary: %v", err)
		}
		t.Fatal("stackit binary not built")
	}

	t.Run("basic foreach on single branch", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, func(s *testhelpers.Scene) error {
			if err := s.Repo.CreateChangeAndCommit("initial", "init"); err != nil {
				return err
			}
			return s.Repo.RunCliCommand([]string{"create", "branch1", "-m", "branch1 change"})
		})

		output, err := scene.Repo.RunCliCommandAndGetOutput([]string{"foreach", "touch", "touched"})

		require.NoError(t, err, "foreach command failed: %s", output)
		require.Contains(t, output, "Running on branch branch1", "should mention branch1")
		require.FileExists(t, scene.Dir+"/touched", "file should be created")
	})

	t.Run("foreach on multiple branches (upstack)", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, func(s *testhelpers.Scene) error {
			if err := s.Repo.CreateChangeAndCommit("initial", "init"); err != nil {
				return err
			}
			// branch1 -> branch2 -> branch3
			for _, b := range []string{"branch1", "branch2", "branch3"} {
				if err := s.Repo.RunCliCommand([]string{"create", b, "-m", b + " change"}); err != nil {
					return err
				}
			}
			// Checkout branch1 to test upstack from there
			return s.Repo.CheckoutBranch("branch1")
		})

		// Test that STACKIT_BRANCH is exported
		output, err := scene.Repo.RunCliCommandAndGetOutput([]string{"foreach", "echo", "branch is $STACKIT_BRANCH"})

		require.NoError(t, err, "foreach command failed: %s", output)
		require.Contains(t, output, "branch is branch1")
		require.Contains(t, output, "branch is branch2")
		require.Contains(t, output, "branch is branch3")
	})

	t.Run("foreach --stack", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, func(s *testhelpers.Scene) error {
			if err := s.Repo.CreateChangeAndCommit("initial", "init"); err != nil {
				return err
			}
			// branch1 -> branch2 -> branch3
			for _, b := range []string{"branch1", "branch2", "branch3"} {
				if err := s.Repo.RunCliCommand([]string{"create", b, "-m", b + " change"}); err != nil {
					return err
				}
			}
			// Checkout branch2
			return s.Repo.CheckoutBranch("branch2")
		})

		// --stack should run on branch1, branch2, branch3
		output, err := scene.Repo.RunCliCommandAndGetOutput([]string{"foreach", "--stack", "echo", "hello"})

		require.NoError(t, err, "foreach command failed: %s", output)
		require.Contains(t, output, "Running on branch branch1")
		require.Contains(t, output, "Running on branch branch2")
		require.Contains(t, output, "Running on branch branch3")
	})

	t.Run("foreach --downstack", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, func(s *testhelpers.Scene) error {
			if err := s.Repo.CreateChangeAndCommit("initial", "init"); err != nil {
				return err
			}
			// branch1 -> branch2 -> branch3
			for _, b := range []string{"branch1", "branch2", "branch3"} {
				if err := s.Repo.RunCliCommand([]string{"create", b, "-m", b + " change"}); err != nil {
					return err
				}
			}
			// Checkout branch2
			return s.Repo.CheckoutBranch("branch2")
		})

		// --downstack should run on branch1, branch2
		output, err := scene.Repo.RunCliCommandAndGetOutput([]string{"foreach", "--downstack", "echo", "hello"})

		require.NoError(t, err, "foreach command failed: %s", output)
		require.Contains(t, output, "Running on branch branch1")
		require.Contains(t, output, "Running on branch branch2")
		require.NotContains(t, output, "Running on branch branch3")
	})

	t.Run("foreach fail-fast", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, func(s *testhelpers.Scene) error {
			if err := s.Repo.CreateChangeAndCommit("initial", "init"); err != nil {
				return err
			}
			// branch1 -> branch2
			for _, b := range []string{"branch1", "branch2"} {
				if err := s.Repo.RunCliCommand([]string{"create", b, "-m", b + " change"}); err != nil {
					return err
				}
			}
			return s.Repo.CheckoutBranch("branch1")
		})

		// fail-fast (default) should stop at branch1
		output, err := scene.Repo.RunCliCommandAndGetOutput([]string{"foreach", "false"})

		require.Error(t, err)
		require.Contains(t, output, "Running on branch branch1")
		require.NotContains(t, output, "Running on branch branch2")
	})

	t.Run("foreach --no-fail-fast", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, func(s *testhelpers.Scene) error {
			if err := s.Repo.CreateChangeAndCommit("initial", "init"); err != nil {
				return err
			}
			// branch1 -> branch2
			for _, b := range []string{"branch1", "branch2"} {
				if err := s.Repo.RunCliCommand([]string{"create", b, "-m", b + " change"}); err != nil {
					return err
				}
			}
			return s.Repo.CheckoutBranch("branch1")
		})

		// --no-fail-fast should continue to branch2 even if branch1 fails
		output, err := scene.Repo.RunCliCommandAndGetOutput([]string{"foreach", "--no-fail-fast", "false"})

		require.NoError(t, err) // Returns nil if --no-fail-fast is used

		require.Contains(t, output, "Running on branch branch1")
		require.Contains(t, output, "Running on branch branch2")
	})

	t.Run("foreach on trunk", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, func(s *testhelpers.Scene) error {
			if err := s.Repo.CreateChangeAndCommit("initial", "init"); err != nil {
				return err
			}
			// branch1
			if err := s.Repo.RunCliCommand([]string{"create", "branch1", "-m", "branch1 change"}); err != nil {
				return err
			}
			// Checkout main
			return s.Repo.CheckoutBranch("main")
		})

		// Running on main should skip main and run on descendants if upstack (default)
		output, err := scene.Repo.RunCliCommandAndGetOutput([]string{"foreach", "echo", "hello"})

		require.NoError(t, err, "foreach command failed: %s", output)
		require.NotContains(t, output, "Running on branch main")
		require.Contains(t, output, "Running on branch branch1")
	})

	t.Run("foreach --parallel", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, func(s *testhelpers.Scene) error {
			if err := s.Repo.CreateChangeAndCommit("initial", "init"); err != nil {
				return err
			}
			// branch1 -> branch2 -> branch3
			for _, b := range []string{"branch1", "branch2", "branch3"} {
				if err := s.Repo.RunCliCommand([]string{"create", b, "-m", b + " change"}); err != nil {
					return err
				}
			}
			return s.Repo.CheckoutBranch("branch1")
		})

		// --parallel should run on all 3 branches
		output, err := scene.Repo.RunCliCommandAndGetOutput([]string{"foreach", "--parallel", "echo", "hi"})

		require.NoError(t, err, "foreach --parallel command failed: %s", output)
		require.Contains(t, output, "Branch: branch1")
		require.Contains(t, output, "Branch: branch2")
		require.Contains(t, output, "Branch: branch3")
		require.Contains(t, output, "hi")
	})

	t.Run("foreach --parallel --jobs", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, func(s *testhelpers.Scene) error {
			if err := s.Repo.CreateChangeAndCommit("initial", "init"); err != nil {
				return err
			}
			for _, b := range []string{"branch1", "branch2"} {
				if err := s.Repo.RunCliCommand([]string{"create", b, "-m", b + " change"}); err != nil {
					return err
				}
			}
			return s.Repo.CheckoutBranch("branch1")
		})

		output, err := scene.Repo.RunCliCommandAndGetOutput([]string{"foreach", "--parallel", "--jobs", "1", "echo", "hi"})

		require.NoError(t, err, "foreach --parallel --jobs command failed: %s", output)
		require.Contains(t, output, "Branch: branch1")
		require.Contains(t, output, "Branch: branch2")
	})
}
