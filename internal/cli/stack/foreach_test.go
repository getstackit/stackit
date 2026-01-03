package stack_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"stackit.dev/stackit/testhelpers"
	"stackit.dev/stackit/testhelpers/scenario"
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

	t.Run("output formatting", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenarioParallel(t, testhelpers.BasicSceneSetup).WithBinaryPath(binaryPath)
		s.RunCli("init")
		s.RunCli("create", "branch1", "-m", "b1")
		s.RunCli("create", "branch2", "-m", "b2")
		s.RunGit("checkout", "branch1")

		// 1. Basic sequential output
		output, err := s.RunCliAndGetOutput("foreach", "echo", "hello")
		require.NoError(t, err)
		normalized := testhelpers.NormalizeOutput(output)
		require.Equal(t, testhelpers.NormalizeOutput(`
Running on branch branch1 (current)...
hello
✓ Command succeeded on branch branch1 (current)
Running on branch branch2...
hello
✓ Command succeeded on branch branch2
Summary:
  ✓ branch1 (current)
    hello
  ✓ branch2
    hello
All branches completed successfully (2 total)
`), normalized)

		// 2. Parallel mode output
		output, err = s.RunCliAndGetOutput("foreach", "--parallel", "echo", "hi")
		require.NoError(t, err)
		normalized = testhelpers.NormalizeOutput(output)
		require.Equal(t, testhelpers.NormalizeOutput(`
Executing in parallel: ..
Summary:
  ✓ branch1 (current)
    hi
  ✓ branch2
    hi
All branches completed successfully (2 total)
`), normalized)

		// 3. Failure output
		output, err = s.RunCliAndGetOutput("foreach", "false")
		require.Error(t, err)
		normalized = testhelpers.NormalizeOutput(output)
		require.Equal(t, testhelpers.NormalizeOutput(`
Running on branch branch1 (current)...
❌ ✗ Command failed on branch branch1 (current): exit status 1
Summary:
❌   ✗ branch1 (current)
❌     Error: exit status 1
Completed: 0 succeeded, 1 failed
Error: command failed on branch branch1: exit status 1
`), normalized)
	})

	t.Run("scope flags", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenarioParallel(t, testhelpers.BasicSceneSetup).WithBinaryPath(binaryPath)
		s.RunCli("init")
		s.RunCli("create", "b1")
		s.RunCli("create", "b2")
		s.RunCli("checkout", "b1")

		// Verify --stack includes everything
		output, _ := s.RunCliAndGetOutput("foreach", "--stack", "echo", "hi")
		require.Equal(t, testhelpers.NormalizeOutput(`
Running on branch b1 (current)...
hi
✓ Command succeeded on branch b1 (current)
Running on branch b2...
hi
✓ Command succeeded on branch b2
Summary:
  ✓ b1 (current)
    hi
  ✓ b2
    hi
All branches completed successfully (2 total)
`), testhelpers.NormalizeOutput(output))

		// Verify default (upstack) only includes b1 and b2
		output, _ = s.RunCliAndGetOutput("foreach", "echo", "hi")
		require.Equal(t, testhelpers.NormalizeOutput(`
Running on branch b1 (current)...
hi
✓ Command succeeded on branch b1 (current)
Running on branch b2...
hi
✓ Command succeeded on branch b2
Summary:
  ✓ b1 (current)
    hi
  ✓ b2
    hi
All branches completed successfully (2 total)
`), testhelpers.NormalizeOutput(output))

		// Verify --downstack from b1 only includes b1
		output, _ = s.RunCliAndGetOutput("foreach", "--downstack", "echo", "hi")
		require.Equal(t, testhelpers.NormalizeOutput(`
Running on branch b1 (current)...
hi
✓ Command succeeded on branch b1 (current)
Summary:
  ✓ b1 (current)
    hi
All branches completed successfully (1 total)
`), testhelpers.NormalizeOutput(output))
	})
}
