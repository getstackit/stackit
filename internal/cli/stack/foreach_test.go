package stack_test

import (
	"os/exec"
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
			return runCliCommand(binaryPath, s.Dir, "create", "branch1", "-m", "branch1 change")
		})

		output := runCliCommandSuccess(t, binaryPath, scene.Dir, "foreach", "echo", "hi")
		normalized := testhelpers.NormalizeOutput(output)

		expected := testhelpers.NormalizeOutput(`
Running on branch branch1 (current)...
hi
✓ Command succeeded on branch branch1 (current)

Summary:
  ✓ branch1 (current)
    hi

All branches completed successfully (1 total)
`)

		require.Equal(t, expected, normalized)
		require.FileExists(t, scene.Dir+"/.git/HEAD", "git should still be valid")
	})

	t.Run("foreach on multiple branches (upstack)", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, func(s *testhelpers.Scene) error {
			if err := s.Repo.CreateChangeAndCommit("initial", "init"); err != nil {
				return err
			}
			// branch1 -> branch2 -> branch3
			for _, b := range []string{"branch1", "branch2", "branch3"} {
				if err := runCliCommand(binaryPath, s.Dir, "create", b, "-m", b+" change"); err != nil {
					return err
				}
			}
			// Checkout branch1 to test upstack from there
			return s.Repo.CheckoutBranch("branch1")
		})

		output := runCliCommandSuccess(t, binaryPath, scene.Dir, "foreach", "echo", "branch is $STACKIT_BRANCH")
		normalized := testhelpers.NormalizeOutput(output)

		expected := testhelpers.NormalizeOutput(`
Running on branch branch1 (current)...
branch is branch1
✓ Command succeeded on branch branch1 (current)

Running on branch branch2...
branch is branch2
✓ Command succeeded on branch branch2

Running on branch branch3...
branch is branch3
✓ Command succeeded on branch branch3

Summary:
  ✓ branch1 (current)
    branch is branch1
  ✓ branch2
    branch is branch2
  ✓ branch3
    branch is branch3

All branches completed successfully (3 total)
`)

		require.Equal(t, expected, normalized)
	})

	t.Run("foreach --stack", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, func(s *testhelpers.Scene) error {
			if err := s.Repo.CreateChangeAndCommit("initial", "init"); err != nil {
				return err
			}
			// branch1 -> branch2 -> branch3
			for _, b := range []string{"branch1", "branch2", "branch3"} {
				if err := runCliCommand(binaryPath, s.Dir, "create", b, "-m", b+" change"); err != nil {
					return err
				}
			}
			// Checkout branch2
			return s.Repo.CheckoutBranch("branch2")
		})

		output := runCliCommandSuccess(t, binaryPath, scene.Dir, "foreach", "--stack", "echo", "hello")
		normalized := testhelpers.NormalizeOutput(output)

		expected := testhelpers.NormalizeOutput(`
Running on branch branch1...
hello
✓ Command succeeded on branch branch1

Running on branch branch2 (current)...
hello
✓ Command succeeded on branch branch2 (current)

Running on branch branch3...
hello
✓ Command succeeded on branch branch3

Summary:
  ✓ branch1
    hello
  ✓ branch2 (current)
    hello
  ✓ branch3
    hello

All branches completed successfully (3 total)
`)

		require.Equal(t, expected, normalized)
	})

	t.Run("foreach --downstack", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, func(s *testhelpers.Scene) error {
			if err := s.Repo.CreateChangeAndCommit("initial", "init"); err != nil {
				return err
			}
			// branch1 -> branch2 -> branch3
			for _, b := range []string{"branch1", "branch2", "branch3"} {
				if err := runCliCommand(binaryPath, s.Dir, "create", b, "-m", b+" change"); err != nil {
					return err
				}
			}
			// Checkout branch2
			return s.Repo.CheckoutBranch("branch2")
		})

		output := runCliCommandSuccess(t, binaryPath, scene.Dir, "foreach", "--downstack", "echo", "hello")
		normalized := testhelpers.NormalizeOutput(output)

		expected := testhelpers.NormalizeOutput(`
Running on branch branch1...
hello
✓ Command succeeded on branch branch1

Running on branch branch2 (current)...
hello
✓ Command succeeded on branch branch2 (current)

Summary:
  ✓ branch1
    hello
  ✓ branch2 (current)
    hello

All branches completed successfully (2 total)
`)

		require.Equal(t, expected, normalized)
	})

	t.Run("foreach fail-fast", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, func(s *testhelpers.Scene) error {
			if err := s.Repo.CreateChangeAndCommit("initial", "init"); err != nil {
				return err
			}
			// branch1 -> branch2
			for _, b := range []string{"branch1", "branch2"} {
				if err := runCliCommand(binaryPath, s.Dir, "create", b, "-m", b+" change"); err != nil {
					return err
				}
			}
			return s.Repo.CheckoutBranch("branch1")
		})

		cmd := exec.Command(binaryPath, "foreach", "false")
		cmd.Dir = scene.Dir
		outputBytes, _ := cmd.CombinedOutput()
		output := string(outputBytes)
		normalized := testhelpers.NormalizeOutput(output)

		expected := testhelpers.NormalizeOutput(`
Running on branch branch1 (current)...
❌ ✗ Command failed on branch branch1 (current): exit status 1

Summary:
❌   ✗ branch1 (current)
❌     Error: exit status 1

Completed: 0 succeeded, 1 failed
Error: command failed on branch branch1: exit status 1
`)

		require.Equal(t, expected, normalized)
	})

	t.Run("foreach --no-fail-fast", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, func(s *testhelpers.Scene) error {
			if err := s.Repo.CreateChangeAndCommit("initial", "init"); err != nil {
				return err
			}
			// branch1 -> branch2
			for _, b := range []string{"branch1", "branch2"} {
				if err := runCliCommand(binaryPath, s.Dir, "create", b, "-m", b+" change"); err != nil {
					return err
				}
			}
			return s.Repo.CheckoutBranch("branch1")
		})

		output := runCliCommandSuccess(t, binaryPath, scene.Dir, "foreach", "--no-fail-fast", "false")
		normalized := testhelpers.NormalizeOutput(output)

		expected := testhelpers.NormalizeOutput(`
Running on branch branch1 (current)...
❌ ✗ Command failed on branch branch1 (current): exit status 1

Running on branch branch2...
❌ ✗ Command failed on branch branch2: exit status 1

Summary:
❌   ✗ branch1 (current)
❌     Error: exit status 1
❌   ✗ branch2
❌     Error: exit status 1

Completed: 0 succeeded, 2 failed
`)

		require.Equal(t, expected, normalized)
	})

	t.Run("foreach on trunk", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, func(s *testhelpers.Scene) error {
			if err := s.Repo.CreateChangeAndCommit("initial", "init"); err != nil {
				return err
			}
			// branch1
			if err := runCliCommand(binaryPath, s.Dir, "create", "branch1", "-m", "branch1 change"); err != nil {
				return err
			}
			// Checkout main
			return s.Repo.CheckoutBranch("main")
		})

		output := runCliCommandSuccess(t, binaryPath, scene.Dir, "foreach", "echo", "hello")
		normalized := testhelpers.NormalizeOutput(output)

		expected := testhelpers.NormalizeOutput(`
Running on branch branch1...
hello
✓ Command succeeded on branch branch1

Summary:
  ✓ branch1
    hello

All branches completed successfully (1 total)
`)

		require.Equal(t, expected, normalized)
	})

	t.Run("foreach --parallel", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, func(s *testhelpers.Scene) error {
			if err := s.Repo.CreateChangeAndCommit("initial", "init"); err != nil {
				return err
			}
			// branch1 -> branch2 -> branch3
			for _, b := range []string{"branch1", "branch2", "branch3"} {
				if err := runCliCommand(binaryPath, s.Dir, "create", b, "-m", b+" change"); err != nil {
					return err
				}
			}
			return s.Repo.CheckoutBranch("branch1")
		})

		output := runCliCommandSuccess(t, binaryPath, scene.Dir, "foreach", "--parallel", "echo", "hi")
		normalized := testhelpers.NormalizeOutput(output)

		// In parallel mode, we show dots for progress.
		expected := testhelpers.NormalizeOutput(`
Executing in parallel: ...
Summary:
  ✓ branch1 (current)
    hi
  ✓ branch2
    hi
  ✓ branch3
    hi

All branches completed successfully (3 total)
`)

		require.Equal(t, expected, normalized)
	})

	t.Run("foreach --parallel --jobs", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, func(s *testhelpers.Scene) error {
			if err := s.Repo.CreateChangeAndCommit("initial", "init"); err != nil {
				return err
			}
			for _, b := range []string{"branch1", "branch2"} {
				if err := runCliCommand(binaryPath, s.Dir, "create", b, "-m", b+" change"); err != nil {
					return err
				}
			}
			return s.Repo.CheckoutBranch("branch1")
		})

		output := runCliCommandSuccess(t, binaryPath, scene.Dir, "foreach", "--parallel", "--jobs", "1", "echo", "hi")
		normalized := testhelpers.NormalizeOutput(output)

		expected := testhelpers.NormalizeOutput(`
Executing in parallel: ..
Summary:
  ✓ branch1 (current)
    hi
  ✓ branch2
    hi

All branches completed successfully (2 total)
`)

		require.Equal(t, expected, normalized)
	})
}

func runCliCommand(binaryPath, dir string, args ...string) error {
	cmd := exec.Command(binaryPath, args...)
	cmd.Dir = dir
	return cmd.Run()
}

func runCliCommandSuccess(t *testing.T, binaryPath, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command(binaryPath, args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "command failed: %s %v\nOutput: %s", binaryPath, args, string(output))
	return string(output)
}
