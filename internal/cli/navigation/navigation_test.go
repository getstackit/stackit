package navigation_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

func TestNavigationCommands(t *testing.T) {
	t.Parallel()

	t.Run("linear stack navigation", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenarioParallel(t, testhelpers.BasicSceneSetup).WithInProcess(true)

		// Create stack: main -> a -> b -> c
		s.RunCli("create", "a", "-m", "a").
			RunCli("create", "b", "-m", "b").
			RunCli("create", "c", "-m", "c")

		// Test parent command from 'c'
		output, err := s.RunCliAndGetOutput("parent")
		require.NoError(t, err)
		require.Equal(t, "b", strings.TrimSpace(output))

		// Test down command
		s.RunCli("down")
		s.ExpectBranch("b")

		// Test parent from 'b'
		output, err = s.RunCliAndGetOutput("parent")
		require.NoError(t, err)
		require.Equal(t, "a", strings.TrimSpace(output))

		// Test children from 'b'
		output, err = s.RunCliAndGetOutput("children")
		require.NoError(t, err)
		require.Equal(t, "c", strings.TrimSpace(output))

		// Test up command
		s.RunCli("up")
		s.ExpectBranch("c")

		// Test children from 'c' (should show no children)
		output, err = s.RunCliAndGetOutput("children")
		require.NoError(t, err)
		require.Contains(t, output, "no children")

		// Test down with steps
		s.RunCli("down", "2")
		s.ExpectBranch("a")

		// Test children from 'a'
		output, err = s.RunCliAndGetOutput("children")
		require.NoError(t, err)
		require.Equal(t, "b", strings.TrimSpace(output))

		// Test up with steps
		s.RunCli("up", "2")
		s.ExpectBranch("c")

		// Test top command from middle
		s.Checkout("a").
			RunCli("top")
		s.ExpectBranch("c")

		// Test bottom command from middle
		s.Checkout("b").
			RunCli("bottom")
		s.ExpectBranch("a")

		// Test trunk command
		output, err = s.RunCliAndGetOutput("trunk")
		require.NoError(t, err)
		require.Equal(t, "main", strings.TrimSpace(output))
	})

	t.Run("quiet down and bottom navigate correctly under light load path", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenarioParallel(t, testhelpers.BasicSceneSetup).WithInProcess(true)

		// Create stack: main -> a -> b -> c
		s.RunCli("create", "a", "-m", "a").
			RunCli("create", "b", "-m", "b").
			RunCli("create", "c", "-m", "c")

		// Quiet down/bottom opt into LoadModeBranchesOnly; verify they still land
		// on the correct branch (parent-chain traversal + checkout) end-to-end.
		s.RunCli("down", "--quiet")
		s.ExpectBranch("b")

		s.RunCli("bottom", "--quiet")
		s.ExpectBranch("a")

		// Multi-step quiet down from the tip walks several parents at once.
		s.Checkout("c").
			RunCli("down", "2", "--quiet")
		s.ExpectBranch("a")
	})

	t.Run("trunk and first branch navigation", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenarioParallel(t, testhelpers.BasicSceneSetup).WithInProcess(true)

		s.RunCli("init")

		// Test trunk command
		output, err := s.RunCliAndGetOutput("trunk")
		require.NoError(t, err)
		require.Equal(t, "main", strings.TrimSpace(output))

		// Test trunk --all
		output, err = s.RunCliAndGetOutput("trunk", "--all")
		require.NoError(t, err)
		require.Contains(t, output, "main (primary)")

		// Test parent on trunk
		output, err = s.RunCliAndGetOutput("parent")
		require.NoError(t, err)
		require.Contains(t, output, "trunk")
		require.Contains(t, output, "no parent")

		// Test down from trunk
		output, err = s.RunCliAndGetOutput("down")
		require.NoError(t, err)
		require.Contains(t, output, "trunk")
		s.ExpectBranch("main")

		// Create first branch
		s.RunCli("create", "a", "-m", "a")

		// Test parent from first branch (should show trunk)
		output, err = s.RunCliAndGetOutput("parent")
		require.NoError(t, err)
		require.Equal(t, "main", strings.TrimSpace(output))

		// Test children from trunk
		s.Checkout("main")
		output, err = s.RunCliAndGetOutput("children")
		require.NoError(t, err)
		require.Equal(t, "a", strings.TrimSpace(output))

		// Test down from first branch (should go to trunk)
		s.Checkout("a").
			RunCli("down")
		s.ExpectBranch("main")

		// Test bottom from first branch
		s.Checkout("a")
		output, err = s.RunCliAndGetOutput("bottom")
		require.NoError(t, err)
		require.Contains(t, output, "Already at the bottom most")
		s.ExpectBranch("a")

		// Test top from first branch
		output, err = s.RunCliAndGetOutput("top")
		require.NoError(t, err)
		require.Contains(t, output, "Already at the top most")
		s.ExpectBranch("a")
	})

	t.Run("forked stack navigation", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenarioParallel(t, testhelpers.BasicSceneSetup).WithInProcess(true)

		// Create branches:
		// main -> a -> b
		//      -> c -> d
		s.RunCli("create", "a", "-m", "a").
			RunCli("create", "b", "-m", "b").
			Checkout("main").
			RunCli("create", "c", "-m", "c").
			RunCli("create", "d", "-m", "d")

		// Test up with --to flag for disambiguation
		s.Checkout("main").
			RunCli("up", "--to", "d")
		s.ExpectBranch("c")

		// Test up with --to b from main
		s.Checkout("main").
			RunCli("up", "--to", "b")
		s.ExpectBranch("a")

		// Test up fails when ambiguous
		s.Checkout("main")
		output, err := s.RunCliAndGetOutput("up")
		require.Error(t, err)
		require.Contains(t, output, "multiple children found")

		// Test up with --to nonexistent fails
		output, err = s.RunCliAndGetOutput("up", "--to", "nonexistent")
		require.Error(t, err)
		require.Contains(t, output, "is not a descendant")

		// Test children shows multiple children
		s.Checkout("main")
		output, err = s.RunCliAndGetOutput("children")
		require.NoError(t, err)
		require.Contains(t, output, "a")
		require.Contains(t, output, "c")

		// Test top with multiple children fails
		s.Checkout("a").
			RunCli("create", "e", "-m", "e").
			Checkout("a")
		output, err = s.RunCliAndGetOutput("top")
		require.Error(t, err)
		require.Contains(t, output, "multiple branches found")
		require.Contains(t, output, "b")
		require.Contains(t, output, "e")
	})

	t.Run("edge cases and error handling", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenarioParallel(t, testhelpers.BasicSceneSetup).WithInProcess(true)

		// Create stack: main -> a -> b
		s.RunCli("create", "a", "-m", "a").
			RunCli("create", "b", "-m", "b")

		// Test down stops early when not enough parents
		s.RunCli("down", "10")
		s.ExpectBranch("main")

		// Test down with invalid steps argument
		s.Checkout("a")
		output, err := s.RunCliAndGetOutput("down", "abc")
		require.Error(t, err)
		require.Contains(t, output, "invalid")

		// Test down with zero steps fails
		output, err = s.RunCliAndGetOutput("down", "0")
		require.Error(t, err)
		require.Contains(t, output, "at least 1")

		// Test down with --steps flag
		s.Checkout("b").
			RunCli("down", "-n", "2")
		s.ExpectBranch("main")
	})

	t.Run("trunk management", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenarioParallel(t, testhelpers.BasicSceneSetup).WithInProcess(true)

		s.RunCli("init")

		// Test trunk --add adds additional trunk
		s.RunGit("checkout", "-b", "develop").
			Checkout("main").
			RunCli("trunk", "--add", "develop")

		output, err := s.RunCliAndGetOutput("trunk", "--all")
		require.NoError(t, err)
		require.Contains(t, output, "main (primary)")
		require.Contains(t, output, "develop")

		// Test trunk --add fails for non-existent branch
		output, err = s.RunCliAndGetOutput("trunk", "--add", "nonexistent")
		require.Error(t, err)
		require.Contains(t, output, "does not exist")

		// Test trunk --add fails for already configured trunk
		output, err = s.RunCliAndGetOutput("trunk", "--add", "main")
		require.Error(t, err)
		require.Contains(t, output, "already")
	})

	// Regression test for #1299: when a branch falls behind after trunk
	// advances, checkout must suggest a command that actually exists. The
	// old strings ("stackit upstack restack" / "stackit stack restack") were
	// not real subcommands.
	t.Run("restack suggestion references real commands when branch falls behind", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenarioParallel(t, testhelpers.BasicSceneSetup).WithInProcess(true)

		// Create stack: main -> a -> b
		s.RunCli("create", "a", "-m", "a").
			RunCli("create", "b", "-m", "b")

		// Advance trunk so the stack falls behind main.
		s.Checkout("main").CommitChange("main-update", "advance trunk")

		// Checking out a (now behind its parent) suggests restacking upstack.
		output, err := s.RunCliAndGetOutput("checkout", "a")
		require.NoError(t, err)
		require.Contains(t, output, "stackit restack --upstack")
		require.NotContains(t, output, "stackit upstack restack")

		// Checking out b (ancestor a is behind) suggests restacking the stack.
		output, err = s.RunCliAndGetOutput("checkout", "b")
		require.NoError(t, err)
		require.Contains(t, output, "The downstack branch")
		require.Contains(t, output, "stackit restack")
		require.NotContains(t, output, "stackit stack restack")
	})
}
