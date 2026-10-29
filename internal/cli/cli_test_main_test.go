package cli_test

import (
	"os"
	"testing"

	"github.com/getstackit/stackit/testhelpers/inprocess"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

func TestMain(m *testing.M) {
	scenario.SetGlobalInProcessRunner(func(workDir string, args ...string) (string, error) {
		runner := inprocess.NewInProcessCLI()
		res := runner.Run(workDir, args...)
		return res.Output, res.Err
	})
	os.Exit(m.Run())
}
