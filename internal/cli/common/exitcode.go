package common

import "fmt"

// ExitCodeError wraps an error from a child process with the exit code stackit
// should return to its caller.
type ExitCodeError struct {
	code int
	err  error
}

func NewExitCodeError(code int, err error) *ExitCodeError {
	return &ExitCodeError{code: code, err: err}
}

func (e *ExitCodeError) Error() string {
	if e.err == nil {
		return fmt.Sprintf("process exited with status %d", e.code)
	}
	return e.err.Error()
}

func (e *ExitCodeError) Unwrap() error {
	return e.err
}

func (e *ExitCodeError) ExitCode() int {
	return e.code
}
