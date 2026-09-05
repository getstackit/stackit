package stack_test

import (
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/testhelpers/inprocess"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

func init() {
	scenario.SetGlobalInProcessRunner(func(workDir string, args ...string) (string, error) {
		result := inprocess.NewInProcessCLI().Run(workDir, args...)
		return result.Output, result.Err
	})
}

func runCliCommand(dir string, args ...string) error {
	_, err := runCliCommandOutput(dir, args...)
	return err
}

func runCliCommandOutput(dir string, args ...string) ([]byte, error) {
	result := inprocess.NewInProcessCLI().Run(dir, args...)
	return []byte(result.Output), result.Err
}

func runCliCommandSuccess(t *testing.T, dir string, args ...string) string {
	t.Helper()
	output, err := runCliCommandOutput(dir, args...)
	require.NoError(t, err, "command failed: stackit %v\nOutput: %s", args, string(output))
	return ansi.Strip(string(output))
}
