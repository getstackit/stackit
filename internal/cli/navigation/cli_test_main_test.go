package navigation_test

import (
	"github.com/getstackit/stackit/testhelpers/inprocess"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

func init() {
	scenario.SetGlobalInProcessRunner(func(workDir string, args ...string) (string, error) {
		result := inprocess.NewInProcessCLI().Run(workDir, args...)
		return result.Output, result.Err
	})
}
