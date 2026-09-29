// Package errors provides error wrapping helpers for consistent error messages.
package errors

import (
	"fmt"
)

// FailedTo wraps an error with a "failed to <action> <target>" message.
// Returns nil if err is nil, making it safe to use in error return statements.
//
// Example:
//
//	return errors.FailedTo("get", "branch revision", err)
//	// Returns: "failed to get branch revision: <underlying error>"
func FailedTo(action, target string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("failed to %s %s: %w", action, target, err)
}
