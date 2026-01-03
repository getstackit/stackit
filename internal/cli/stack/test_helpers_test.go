package stack_test

import (
	"os/exec"
	"testing"

	"github.com/stretchr/testify/require"
)

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
