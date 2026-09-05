package branch_test

import "github.com/getstackit/stackit/testhelpers/inprocess"

func runCLI(dir string, args ...string) ([]byte, error) {
	result := inprocess.NewInProcessCLI().Run(dir, args...)
	return []byte(result.Output), result.Err
}
