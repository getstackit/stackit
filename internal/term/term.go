// Package term reports whether stackit is attached to an interactive
// terminal and holds the process-wide interactive mode.
package term

import (
	"errors"
	"io"
	"os"
	"strings"
	"sync/atomic"

	"github.com/mattn/go-isatty"
)

const (
	// termTypeDumb is the TERM value for dumb terminals that don't support control codes.
	termTypeDumb = "dumb"

	// envFalse is the canonical false string for boolean environment variables.
	envFalse = "false"
)

// nonInteractive is inverted so the zero value means interactive
var nonInteractive atomic.Bool

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

// SetInteractive sets whether the TUI should be interactive
func SetInteractive(interactive bool) {
	nonInteractive.Store(!interactive)
}

// TerminalDetected reports whether a usable interactive terminal is attached,
// independent of the configured interactive mode. It checks the TERM
// environment, stdin/stdout isatty, and /dev/tty availability. Use this when
// resolving the default interactivity before the interactive mode is set (e.g.
// in the root command), where IsTTY's mode gate would not yet apply.
func TerminalDetected() bool {
	if !supportsTerminalControl() {
		return false
	}
	// First check if stdin/stdout are terminals
	if (!isatty.IsTerminal(os.Stdin.Fd()) && !isatty.IsCygwinTerminal(os.Stdin.Fd())) ||
		(!isatty.IsTerminal(os.Stdout.Fd()) && !isatty.IsCygwinTerminal(os.Stdout.Fd())) {
		return false
	}
	// Also try to open /dev/tty to verify it's actually available
	f, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return false
	}
	_ = f.Close()
	return true
}

// NonInteractiveEnv reports whether STACKIT_NO_INTERACTIVE requests
// non-interactive mode. Any value other than empty/"0"/"false"/"no" enables it,
// letting agents and scripts opt out of prompts once instead of passing
// --no-interactive on every command.
func NonInteractiveEnv() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("STACKIT_NO_INTERACTIVE"))) {
	case "", "0", envFalse, "no":
		return false
	default:
		return true
	}
}

func supportsTerminalControl() bool {
	term := strings.ToLower(os.Getenv("TERM"))
	if term == "" || term == termTypeDumb || strings.HasPrefix(term, termTypeDumb+"-") {
		return false
	}
	return true
}

// IsInteractive reports whether prompts and the TUI may be used: interactive
// mode is on and a usable terminal is attached.
func IsInteractive() bool {
	if nonInteractive.Load() {
		return false
	}
	return TerminalDetected()
}

// ReadStdin reads all piped standard input. It returns "" without blocking
// when stdin is a terminal or an empty file.
func ReadStdin() (string, error) {
	stat, err := os.Stdin.Stat()
	if err != nil {
		return "", err
	}

	// If it's a terminal, we don't want to block waiting for input
	if (stat.Mode() & os.ModeCharDevice) != 0 {
		return "", nil
	}

	// If it's a regular file and it's empty, return empty (don't block)
	if stat.Mode().IsRegular() && stat.Size() == 0 {
		return "", nil
	}

	bytes, err := io.ReadAll(os.Stdin)
	if err != nil {
		return "", err
	}

	result := strings.TrimSpace(string(bytes))
	return result, nil
}
