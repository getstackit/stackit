package rerere

import (
	"context"
	"strings"

	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/internal/tui"
)

const declinedKey = "stackit.rerere.declined"

// ConfirmFunc prompts the user for a yes/no answer. Matches the signature
// of tui.PromptConfirm.
type ConfirmFunc func(prompt string, defaultValue bool) (bool, error)

// Pauser releases and restores an active TUI so a prompt can read the
// terminal without contending with a running Bubble Tea program.
type Pauser interface {
	Pause()
	Resume()
}

// EnsureEnabled offers to enable git rerere for this repository. It never
// prompts when interactive is false or the user previously declined.
//
// When rerere.enabled is already true, it still ensures rerere.autoupdate
// is true — stackit's auto-continue loop (see internal/git/rebase.go) relies
// on rerere staging resolutions itself, and silently no-ops when autoupdate
// is off.
//
// If pauser is non-nil, it is paused around the confirmation prompt so a
// surrounding TUI does not contend for stdin/stdout.
func EnsureEnabled(ctx context.Context, runner git.Runner, interactive bool, pauser Pauser) (bool, error) {
	return ensureEnabled(ctx, runner, interactive, pauser, tui.PromptConfirm)
}

func ensureEnabled(_ context.Context, runner git.Runner, interactive bool, pauser Pauser, confirm ConfirmFunc) (bool, error) {
	if configBool(runner, "rerere.enabled") {
		if !configBool(runner, "rerere.autoupdate") {
			if err := runner.SetConfig("rerere.autoupdate", "true"); err != nil {
				return false, err
			}
		}
		return false, nil
	}

	if configBool(runner, declinedKey) || !interactive {
		return false, nil
	}

	ok, err := promptWithPause(pauser, confirm, "Enable git rerere to remember conflict resolutions?", true)
	if err != nil {
		return false, nil //nolint:nilerr // prompt cancel (Ctrl+C) should not error — treat as decline without persisting
	}
	if !ok {
		_ = runner.SetConfig(declinedKey, "true")
		return false, nil
	}

	if err := runner.SetConfig("rerere.enabled", "true"); err != nil {
		return false, err
	}
	if err := runner.SetConfig("rerere.autoupdate", "true"); err != nil {
		return false, err
	}
	return true, nil
}

func promptWithPause(pauser Pauser, confirm ConfirmFunc, prompt string, defaultValue bool) (bool, error) {
	if pauser != nil {
		pauser.Pause()
		defer pauser.Resume()
	}
	return confirm(prompt, defaultValue)
}

func configBool(runner git.Runner, key string) bool {
	value, err := runner.GetConfig(key)
	if err != nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(value), "true")
}
