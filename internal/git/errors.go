package git

import (
	"errors"
	"fmt"
	"strings"
)

// CommandError represents an error from a git command execution
type CommandError struct {
	Command string
	Args    []string
	Stdout  string
	Stderr  string
	Err     error
}

func (e *CommandError) Error() string {
	msg := fmt.Sprintf("git command failed: %s", e.Command)
	if len(e.Args) > 0 {
		msg += fmt.Sprintf(" %v", e.Args)
	}
	if e.Stderr != "" {
		msg += fmt.Sprintf("\nstderr: %s", e.Stderr)
	}
	if e.Stdout != "" {
		msg += fmt.Sprintf("\nstdout: %s", e.Stdout)
	}
	if e.Err != nil {
		msg += fmt.Sprintf("\n%v", e.Err)
	}
	return msg
}

func (e *CommandError) Unwrap() error {
	return e.Err
}

// NewCommandError creates a new CommandError
func NewCommandError(command string, args []string, stdout, stderr string, err error) *CommandError {
	return &CommandError{
		Command: command,
		Args:    args,
		Stdout:  stdout,
		Stderr:  stderr,
		Err:     err,
	}
}

// Git command name constants used across multiple files in the git package.
const (
	gitCmdPush    = "push"
	gitCmdDiff    = "diff"
	gitEditorTrue = "GIT_EDITOR=true"
)

// IsBranchNotFoundError returns true if the error indicates that a branch was not found
func IsBranchNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	var ce *CommandError
	if errors.As(err, &ce) {
		return strings.Contains(ce.Stderr, "not found") || strings.Contains(ce.Stderr, "does not exist")
	}
	return false
}

// IsLocalChangesError returns true if the error indicates that local changes would be overwritten
func IsLocalChangesError(err error) bool {
	if err == nil {
		return false
	}
	var ce *CommandError
	if errors.As(err, &ce) {
		return strings.Contains(ce.Stderr, "Your local changes to the following files would be overwritten by checkout") ||
			strings.Contains(ce.Stderr, "Your local changes to the following files would be overwritten by merge")
	}
	return false
}
