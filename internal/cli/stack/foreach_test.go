package stack_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/actions/foreach"
	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
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

		// 3. --jobs implies parallel mode
		output, err = s.RunCliAndGetOutput("foreach", "--jobs", "2", "echo", "jobs")
		require.NoError(t, err)
		normalized = testhelpers.NormalizeOutput(output)
		require.Equal(t, testhelpers.NormalizeOutput(`
Executing in parallel: ..
Summary:
  ✓ branch1 (current)
    jobs
  ✓ branch2
    jobs
All branches completed successfully (2 total)
`), normalized)

		// 4. Failure output
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

	t.Run("branch flag anchors traversal", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenarioParallel(t, testhelpers.BasicSceneSetup).WithBinaryPath(binaryPath)
		s.RunCli("init")
		s.RunCli("create", "b1")
		s.RunCli("create", "b2")
		s.RunCli("create", "b3")
		s.RunCli("checkout", "b3")

		output, err := s.RunCliAndGetOutput("foreach", "--branch", "b1", "--upstack", "echo", "$STACKIT_BRANCH")
		require.NoError(t, err)
		require.Equal(t, testhelpers.NormalizeOutput(`
Running on branch b1...
b1
✓ Command succeeded on branch b1
Running on branch b2...
b2
✓ Command succeeded on branch b2
Running on branch b3 (current)...
b3
✓ Command succeeded on branch b3 (current)
Summary:
  ✓ b1
    b1
  ✓ b2
    b2
  ✓ b3 (current)
    b3
All branches completed successfully (3 total)
`), testhelpers.NormalizeOutput(output))

		s.ExpectBranch("b3")
	})

	t.Run("json output reports branch results", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenarioParallel(t, testhelpers.BasicSceneSetup).WithBinaryPath(binaryPath)
		s.RunCli("init")
		s.RunCli("create", "b1")
		s.RunCli("create", "b2")
		s.RunCli("checkout", "b1")

		output, err := s.RunCliAndGetOutput("foreach", "--json", "echo", "$STACKIT_BRANCH")
		require.NoError(t, err)

		var result foreach.JSONResult
		require.NoError(t, json.Unmarshal([]byte(output), &result))
		require.Equal(t, foreach.JSONStatusSuccess, result.Status)
		require.Equal(t, "echo $STACKIT_BRANCH", result.Command)
		require.Equal(t, 2, result.TotalCount)
		require.Equal(t, 2, result.SuccessCount)
		require.Equal(t, 0, result.FailureCount)
		require.Equal(t, []foreach.JSONBranchResult{
			{Branch: "b1", Status: foreach.StatusDone, ExitCode: 0, Output: "b1\n"},
			{Branch: "b2", Status: foreach.StatusDone, ExitCode: 0, Output: "b2\n"},
		}, result.Results)
	})

	t.Run("json output reports command failures", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenarioParallel(t, testhelpers.BasicSceneSetup).WithBinaryPath(binaryPath)
		s.RunCli("init")
		s.RunCli("create", "b1")
		s.RunCli("create", "b2")
		s.RunCli("checkout", "b1")

		output, err := s.RunCliAndGetOutput("foreach", "--json", "--no-fail-fast", "false")
		require.NoError(t, err)

		var result foreach.JSONResult
		require.NoError(t, json.Unmarshal([]byte(output), &result))
		require.Equal(t, foreach.JSONStatusFailure, result.Status)
		require.Equal(t, 2, result.TotalCount)
		require.Equal(t, 0, result.SuccessCount)
		require.Equal(t, 2, result.FailureCount)
		require.Len(t, result.Results, 2)
		for _, branchResult := range result.Results {
			require.Equal(t, foreach.StatusError, branchResult.Status)
			require.Equal(t, 1, branchResult.ExitCode)
			require.Contains(t, branchResult.Error, "exit status 1")
		}
	})

	t.Run("find-first-failure json stops before descendants", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenarioParallel(t, testhelpers.BasicSceneSetup).WithBinaryPath(binaryPath)
		s.RunCli("init")
		s.RunCli("create", "root")
		s.RunCli("create", "child-a")
		s.RunCli("checkout", "root")
		s.RunCli("create", "child-b")
		s.RunCli("checkout", "child-a")
		s.RunCli("create", "grandchild-a")
		s.RunCli("checkout", "root")

		command := "case \"$STACKIT_BRANCH\" in child-a|child-b) exit 1;; *) echo $STACKIT_BRANCH;; esac"
		output, err := s.RunCliAndGetOutput("foreach", "--json", "--find-first-failure", "--jobs", "2", command)
		require.Error(t, err)

		var result foreach.JSONResult
		require.NoError(t, json.Unmarshal([]byte(output), &result))
		require.Equal(t, foreach.JSONStatusFailure, result.Status)
		require.Equal(t, 3, result.TotalCount)
		require.Equal(t, 1, result.SuccessCount)
		require.Equal(t, 2, result.FailureCount)
		require.Equal(t, []string{"root", "child-a", "child-b"}, jsonBranchNames(result.Results))
	})

	t.Run("all-stacks runs across every independent stack", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenarioParallel(t, testhelpers.BasicSceneSetup).WithBinaryPath(binaryPath)
		s.RunCli("init")

		// Stack A: main -> alpha-root -> alpha-child
		s.RunCli("create", "alpha-root", "-m", "alpha root")
		s.RunCli("create", "alpha-child", "-m", "alpha child")

		// Stack B: main -> beta-root (independent of alpha)
		s.RunCli("checkout", "main")
		s.RunCli("create", "beta-root", "-m", "beta root")

		// Invoke from beta-root — with default upstack scope this would only see beta-root.
		output, err := s.RunCliAndGetOutput("foreach", "--all-stacks", "--json", "echo", "$STACKIT_BRANCH")
		require.NoError(t, err)

		var result foreach.JSONResult
		require.NoError(t, json.Unmarshal([]byte(output), &result))
		require.Equal(t, foreach.JSONStatusSuccess, result.Status)
		require.ElementsMatch(t, []string{"alpha-root", "alpha-child", "beta-root"}, jsonBranchNames(result.Results))
	})

	t.Run("stacks filters to the selected independent roots", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenarioParallel(t, testhelpers.BasicSceneSetup).WithBinaryPath(binaryPath)
		s.RunCli("init")

		// Two independent stacks.
		s.RunCli("create", "alpha-root", "-m", "alpha root")
		s.RunCli("create", "alpha-child", "-m", "alpha child")
		s.RunCli("checkout", "main")
		s.RunCli("create", "beta-root", "-m", "beta root")

		output, err := s.RunCliAndGetOutput("foreach", "--stacks", "alpha-root", "--json", "echo", "$STACKIT_BRANCH")
		require.NoError(t, err)

		var result foreach.JSONResult
		require.NoError(t, json.Unmarshal([]byte(output), &result))
		require.Equal(t, foreach.JSONStatusSuccess, result.Status)
		require.ElementsMatch(t, []string{"alpha-root", "alpha-child"}, jsonBranchNames(result.Results))
	})

	t.Run("all-stacks rejects --stacks, --branch, and scope flags", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenarioParallel(t, testhelpers.BasicSceneSetup).WithBinaryPath(binaryPath)
		s.RunCli("init")
		s.RunCli("create", "root", "-m", "r")

		// --all-stacks and --stacks are mutually exclusive.
		_, err := s.RunCliAndGetOutput("foreach", "--all-stacks", "--stacks", "root", "echo", "x")
		require.Error(t, err)

		// --branch can't be combined with multi-stack scopes.
		_, err = s.RunCliAndGetOutput("foreach", "--all-stacks", "--branch", "root", "echo", "x")
		require.Error(t, err)

		// Explicit per-branch scope flag conflicts with --all-stacks.
		_, err = s.RunCliAndGetOutput("foreach", "--all-stacks", "--stack", "echo", "x")
		require.Error(t, err)

		// Unknown stack root surfaces as an action-level error.
		_, err = s.RunCliAndGetOutput("foreach", "--stacks", "does-not-exist", "echo", "x")
		require.Error(t, err)
	})
}

func jsonBranchNames(results []foreach.JSONBranchResult) []string {
	names := make([]string, len(results))
	for i, result := range results {
		names[i] = result.Branch
	}
	return names
}
