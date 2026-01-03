package cli_test

import (
	"testing"

	"stackit.dev/stackit/testhelpers"
	"stackit.dev/stackit/testhelpers/inprocess"
	"stackit.dev/stackit/testhelpers/scenario"
)

func TestMain(m *testing.M) {
	scenario.SetGlobalInProcessRunner(func(workDir string, args ...string) (string, error) {
		runner := inprocess.NewInProcessCLI()
		res := runner.Run(workDir, args...)
		return res.Output, res.Err
	})
	testhelpers.TestMain(m, nil)
}
