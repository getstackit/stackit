package stack_test

import (
	"os/exec"
	"testing"

	"github.com/stretchr/testify/require"

	"stackit.dev/stackit/testhelpers"
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
		scene := testhelpers.NewSceneParallel(t, nil)

		err := scene.Repo.CreateChangeAndCommit("initial", "init")
		require.NoError(t, err)

		cmd := exec.Command(binaryPath, "init")
		cmd.Dir = scene.Dir
		_, err = cmd.CombinedOutput()
		require.NoError(t, err)

		cmd = exec.Command(binaryPath, "create", "branch-a")
		cmd.Dir = scene.Dir
		_, err = cmd.CombinedOutput()
		require.NoError(t, err)

		cmd = exec.Command(binaryPath, "create", "branch-b")
		cmd.Dir = scene.Dir
		_, err = cmd.CombinedOutput()
		require.NoError(t, err)

		cmd = exec.Command(binaryPath, "create", "branch-c")
		cmd.Dir = scene.Dir
		_, err = cmd.CombinedOutput()
		require.NoError(t, err)

		err = scene.Repo.CheckoutBranch("branch-b")
		require.NoError(t, err)

		cmd = exec.Command(binaryPath, "submit", "--dry-run", "--no-edit", "--draft", "--no-interactive")
		cmd.Dir = scene.Dir
		output, err := cmd.CombinedOutput()
		require.NoError(t, err, "submit failed: %s", string(output))

		normalized := testhelpers.NormalizeOutput(string(output))

		expected := testhelpers.NormalizeOutput(`
Stack to submit:
  branch-a
● branch-b
⚠️  The following branches have no changes:
⚠️  ▸ branch-a
⚠️  ▸ branch-b
⚠️  Are you sure you want to submit them?
  ▸ branch-a → create
  ▸ branch-b (current) → create
`)

		require.Equal(t, expected, normalized, "output format should match expected structure")
	})

	t.Run("submit with --stack includes descendants", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)

		err := scene.Repo.CreateChangeAndCommit("initial", "init")
		require.NoError(t, err)

		cmd := exec.Command(binaryPath, "init")
		cmd.Dir = scene.Dir
		_, err = cmd.CombinedOutput()
		require.NoError(t, err)

		cmd = exec.Command(binaryPath, "create", "branch1")
		cmd.Dir = scene.Dir
		_, err = cmd.CombinedOutput()
		require.NoError(t, err)

		cmd = exec.Command(binaryPath, "create", "branch2")
		cmd.Dir = scene.Dir
		_, err = cmd.CombinedOutput()
		require.NoError(t, err)

		cmd = exec.Command(binaryPath, "create", "branch3")
		cmd.Dir = scene.Dir
		_, err = cmd.CombinedOutput()
		require.NoError(t, err)

		err = scene.Repo.CheckoutBranch("branch2")
		require.NoError(t, err)

		cmd = exec.Command(binaryPath, "submit", "--stack", "--dry-run", "--no-edit", "--draft", "--no-interactive")
		cmd.Dir = scene.Dir
		output, err := cmd.CombinedOutput()
		require.NoError(t, err, "submit command failed: %s", string(output))

		normalized := testhelpers.NormalizeOutput(string(output))

		expected := testhelpers.NormalizeOutput(`
Stack to submit:
  branch1
● branch2
  branch3
⚠️  The following branches have no changes:
⚠️  ▸ branch1
⚠️  ▸ branch2
⚠️  ▸ branch3
⚠️  Are you sure you want to submit them?
  ▸ branch1 → create
  ▸ branch2 (current) → create
  ▸ branch3 → create
`)

		require.Equal(t, expected, normalized, "output should include all branches in stack")
	})

	t.Run("ss alias works like submit --stack", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)

		err := scene.Repo.CreateChangeAndCommit("initial", "init")
		require.NoError(t, err)

		cmd := exec.Command(binaryPath, "init")
		cmd.Dir = scene.Dir
		_, err = cmd.CombinedOutput()
		require.NoError(t, err)

		cmd = exec.Command(binaryPath, "create", "branch1")
		cmd.Dir = scene.Dir
		_, err = cmd.CombinedOutput()
		require.NoError(t, err)

		cmd = exec.Command(binaryPath, "create", "branch2")
		cmd.Dir = scene.Dir
		_, err = cmd.CombinedOutput()
		require.NoError(t, err)

		cmd = exec.Command(binaryPath, "create", "branch3")
		cmd.Dir = scene.Dir
		_, err = cmd.CombinedOutput()
		require.NoError(t, err)

		err = scene.Repo.CheckoutBranch("branch2")
		require.NoError(t, err)

		cmd = exec.Command(binaryPath, "ss", "--dry-run", "--no-edit", "--draft", "--no-interactive")
		cmd.Dir = scene.Dir
		output, err := cmd.CombinedOutput()
		require.NoError(t, err, "ss alias failed: %s", string(output))

		normalized := testhelpers.NormalizeOutput(string(output))

		expected := testhelpers.NormalizeOutput(`
Stack to submit:
  branch1
● branch2
  branch3
⚠️  The following branches have no changes:
⚠️  ▸ branch1
⚠️  ▸ branch2
⚠️  ▸ branch3
⚠️  Are you sure you want to submit them?
  ▸ branch1 → create
  ▸ branch2 (current) → create
  ▸ branch3 → create
`)

		require.Equal(t, expected, normalized, "ss alias should behave like submit --stack")
	})
}
