package utils

import "errors"

// ErrInteractiveDisabled is returned when interactive prompts are disabled
// (running with --no-interactive or not in a TTY).
var ErrInteractiveDisabled = errors.New("interactive prompts are disabled (running with --no-interactive or not in a TTY)")

// CheckInteractiveAllowed returns ErrInteractiveDisabled when interactive
// prompts are not available, and nil otherwise.
func CheckInteractiveAllowed() error {
	if !IsInteractive() {
		return ErrInteractiveDisabled
	}
	return nil
}
