package stack_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/engine"
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
Submit plan → main
Will submit (2)
  branch-a → create (empty)
● branch-b → create (empty)
Dry run complete
`)
		require.Equal(t, expected, testhelpers.NormalizeOutput(output))

		// 2. Submit --stack (full stack)
		output = runCliCommandSuccess(t, binaryPath, scene.Dir, "submit", "--stack", "--dry-run", "--no-edit", "--draft", "--no-interactive")
		expectedStack := testhelpers.NormalizeOutput(`
Submit plan → main
Will submit (3)
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

	t.Run("all up to date output format", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, func(s *testhelpers.Scene) error {
			if err := testhelpers.InitialCommitSceneSetup(s); err != nil {
				return err
			}
			runCliCommand(binaryPath, s.Dir, "init")
			for _, b := range []string{"branch-a", "branch-b", "branch-c"} {
				runCliCommand(binaryPath, s.Dir, "create", b)
			}
			if _, err := s.Repo.CreateBareRemote("origin"); err != nil {
				return err
			}
			for _, b := range []string{"main", "branch-a", "branch-b", "branch-c"} {
				if err := s.Repo.PushBranch("origin", b); err != nil {
					return err
				}
			}

			eng, err := engine.NewEngine(engine.Options{RepoRoot: s.Dir, Trunk: "main"})
			if err != nil {
				return err
			}
			prs := map[string]*engine.PrInfo{
				"branch-a": testhelpers.NewTestPrInfoFull(101, "", "", "OPEN", "main", "https://github.com/getstackit/stackit/pull/101", false),
				"branch-b": testhelpers.NewTestPrInfoFull(102, "", "", "OPEN", "branch-a", "https://github.com/getstackit/stackit/pull/102", false),
				"branch-c": testhelpers.NewTestPrInfoFull(103, "", "", "OPEN", "branch-b", "https://github.com/getstackit/stackit/pull/103", false),
			}
			for name, prInfo := range prs {
				if err := eng.UpsertPrInfo(context.Background(), eng.GetBranch(name), prInfo); err != nil {
					return err
				}
			}
			return s.Repo.CheckoutBranch("branch-b")
		})

		output := runCliCommandSuccess(t, binaryPath, scene.Dir, "ss", "--no-edit", "--no-interactive")
		expected := testhelpers.NormalizeOutput(`
Submit plan → main
No changes (3)
  branch-a
● branch-b
  branch-c
All PRs up to date
`)
		require.Equal(t, expected, testhelpers.NormalizeOutput(output))
	})

	t.Run("solo branch drops stack framing", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, func(s *testhelpers.Scene) error {
			if err := testhelpers.InitialCommitSceneSetup(s); err != nil {
				return err
			}
			runCliCommand(binaryPath, s.Dir, "init")
			runCliCommand(binaryPath, s.Dir, "create", "solo-branch")
			return nil
		})

		// A lone branch off trunk reads as "name → base", with no "Stack to
		// submit" header.
		output := runCliCommandSuccess(t, binaryPath, scene.Dir, "submit", "--dry-run", "--no-edit", "--draft", "--no-interactive")
		expected := testhelpers.NormalizeOutput(`
solo-branch → main  create (empty)
Dry run complete
`)
		require.Equal(t, expected, testhelpers.NormalizeOutput(output))
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

		// On trunk with no stacked branches there is nothing to submit. Standing
		// on trunk is surfaced distinctly: the action points the user at creating
		// or checking out a branch rather than printing a bare "nothing to submit".
		output := runCliCommandSuccess(t, binaryPath, scene.Dir, "submit", "--no-edit", "--no-interactive")
		expected := testhelpers.NormalizeOutput(`
You're on main — nothing to submit from here.
💡 Check out a branch to submit its stack, or create one:
  stackit checkout <branch>
  stackit create -m "feat: ..."
`)
		require.Equal(t, expected, testhelpers.NormalizeOutput(output))
	})
}
