package stack_test

import (
	"os/exec"
	"testing"

	"github.com/stretchr/testify/require"

	"stackit.dev/stackit/testhelpers"
)

func TestMoveCommand(t *testing.T) {
	t.Parallel()
	binaryPath := testhelpers.GetSharedBinaryPath()
	if binaryPath == "" {
		if err := testhelpers.GetBinaryError(); err != nil {
			t.Fatalf("failed to build stackit binary: %v", err)
		}
		t.Fatal("stackit binary not built")
	}

	t.Run("successful moves", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, func(s *testhelpers.Scene) error {
			if err := s.Repo.CreateChangeAndCommit("initial", "init"); err != nil {
				return err
			}
			runCliCommand(binaryPath, s.Dir, "init")
			// Create branch1 -> branch2 -> branch3
			for _, b := range []string{"branch1", "branch2", "branch3"} {
				if err := s.Repo.CreateChange(b+" change", b, false); err != nil {
					return err
				}
				runCliCommand(binaryPath, s.Dir, "create", b, "-m", b+" change")
			}
			return nil
		})

		// 1. Move branch2 to main (downstack)
		output := runCliCommandSuccess(t, binaryPath, scene.Dir, "move", "--source", "branch2", "--onto", "main")
		normalized := testhelpers.NormalizeOutput(output)
		require.Equal(t, testhelpers.NormalizeOutput(`
Moved branch2 (current) from branch1 to main.
Restacked branch2 on main.
Restacked branch3 (current) on branch2.
`), normalized)

		// 2. Move current branch (branch3) to branch1
		err := scene.Repo.CheckoutBranch("branch3")
		require.NoError(t, err)
		output = runCliCommandSuccess(t, binaryPath, scene.Dir, "move", "--onto", "branch1")
		require.Equal(t, testhelpers.NormalizeOutput(`
Moved branch3 (current) from branch2 to branch1.
Restacked branch3 (current) on branch1.
`), testhelpers.NormalizeOutput(output))

		// 3. Move across different stack trees and restack
		// Setup: main -> branch2
		//        main -> branch1 -> branch3
		// Make change to main
		err = scene.Repo.CheckoutBranch("main")
		require.NoError(t, err)
		err = scene.Repo.CreateChangeAndCommit("main change", "main")
		require.NoError(t, err)

		output = runCliCommandSuccess(t, binaryPath, scene.Dir, "move", "--source", "branch1", "--onto", "main")
		require.Equal(t, testhelpers.NormalizeOutput(`
Moved branch1 (current) from main to main.
Restacked branch1 on main.
Restacked branch3 on branch1.
`), testhelpers.NormalizeOutput(output))
	})

	t.Run("invalid moves", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, func(s *testhelpers.Scene) error {
			if err := s.Repo.CreateChangeAndCommit("initial", "init"); err != nil {
				return err
			}
			runCliCommand(binaryPath, s.Dir, "init")
			runCliCommand(binaryPath, s.Dir, "create", "branch1", "-m", "branch1")
			runCliCommand(binaryPath, s.Dir, "create", "branch2", "-m", "branch2")
			return nil
		})

		testCases := []struct {
			name     string
			args     []string
			expected string
		}{
			{
				name:     "move trunk",
				args:     []string{"move", "--source", "main", "--onto", "main"},
				expected: "Error: cannot move trunk branch",
			},
			{
				name:     "move onto descendant",
				args:     []string{"move", "--source", "branch1", "--onto", "branch2"},
				expected: "Error: cannot move branch1 onto its own descendant branch2",
			},
			{
				name:     "move onto itself",
				args:     []string{"move", "--source", "branch1", "--onto", "branch1"},
				expected: "Error: cannot move branch onto itself",
			},
			{
				name:     "onto branch does not exist",
				args:     []string{"move", "--source", "branch1", "--onto", "nonexistent"},
				expected: "Error: branch nonexistent does not exist",
			},
			{
				name:     "source branch not tracked",
				args:     []string{"move", "--source", "untracked", "--onto", "main"},
				expected: "Error: branch untracked is not tracked by Stackit",
			},
		}

		// Create untracked branch for one of the cases
		err := scene.Repo.CreateAndCheckoutBranch("untracked")
		require.NoError(t, err)

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				cmd := exec.Command(binaryPath, tc.args...)
				cmd.Dir = scene.Dir
				output, err := cmd.CombinedOutput()
				require.Error(t, err)
				require.Contains(t, string(output), tc.expected)
			})
		}
	})
}
