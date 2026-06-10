package stack_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/testhelpers"
)

func TestSubmitCommand(t *testing.T) {
	t.Parallel()
	binaryPath := testhelpers.GetSharedBinaryPath()
	if binaryPath == "" {
		if err := testhelpers.GetBinaryError(); err != nil {
			t.Fatalf("failed to build stackit binary: %v", err)
		}
		t.Fatal("stackit binary not built")
	}

	t.Run("dry-run output format", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, func(s *testhelpers.Scene) error {
			if err := testhelpers.InitialCommitSceneSetup(s); err != nil {
				return err
			}
			runCliCommand(binaryPath, s.Dir, "init")
			for _, b := range []string{"branch-a", "branch-b", "branch-c"} {
				runCliCommand(binaryPath, s.Dir, "create", b)
			}
			return s.Repo.CheckoutBranch("branch-b")
		})

		// 1. Basic submit (downstack)
		output := runCliCommandSuccess(t, binaryPath, scene.Dir, "submit", "--dry-run", "--no-edit", "--draft", "--no-interactive")
		expected := testhelpers.NormalizeOutput(`
Stack to submit:
  branch-a → create (empty)
● branch-b → create (empty)
Dry run complete
`)
		require.Equal(t, expected, testhelpers.NormalizeOutput(output))

		// 2. Submit --stack (full stack)
		output = runCliCommandSuccess(t, binaryPath, scene.Dir, "submit", "--stack", "--dry-run", "--no-edit", "--draft", "--no-interactive")
		expectedStack := testhelpers.NormalizeOutput(`
Stack to submit:
  branch-a → create (empty)
● branch-b → create (empty)
  branch-c → create (empty)
Dry run complete
`)
		require.Equal(t, expectedStack, testhelpers.NormalizeOutput(output))

		// 3. ss alias (same as submit --stack)
		output = runCliCommandSuccess(t, binaryPath, scene.Dir, "ss", "--dry-run", "--no-edit", "--draft", "--no-interactive")
		require.Equal(t, expectedStack, testhelpers.NormalizeOutput(output), "ss alias should behave like submit --stack")
	})

	t.Run("no branches to submit reports cleanly", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, func(s *testhelpers.Scene) error {
			if err := testhelpers.InitialCommitSceneSetup(s); err != nil {
				return err
			}
			runCliCommand(binaryPath, s.Dir, "init")
			return nil
		})

		// On trunk with no stacked branches there is nothing to submit. Action
		// is the single source of truth now (HasSubmitWork was folded in), so it
		// must still surface the outcome.
		output := runCliCommandSuccess(t, binaryPath, scene.Dir, "submit", "--no-edit", "--no-interactive")
		require.Contains(t, testhelpers.NormalizeOutput(output), "No branches to submit")
	})
}
