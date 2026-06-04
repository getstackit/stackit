package rerere

import (
	"context"
	"strings"

	"github.com/getstackit/stackit/internal/tui"
)

const declinedKey = "stackit.rerere.declined"

// GitConfigurer reads and writes git configuration values. Both git.Runner and
// engine.Engine satisfy it, so callers can pass the engine rather than reaching
// for the raw git runner.
type GitConfigurer interface {
	GetConfig(key string) (string, error)
	SetConfig(key, value string) error
}

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
func EnsureEnabled(ctx context.Context, cfg GitConfigurer, interactive bool, pauser Pauser) (bool, error) {
	return ensureEnabled(ctx, cfg, interactive, pauser, tui.PromptConfirm)
}

func ensureEnabled(_ context.Context, cfg GitConfigurer, interactive bool, pauser Pauser, confirm ConfirmFunc) (bool, error) {
	if configBool(cfg, "rerere.enabled") {
		if !configBool(cfg, "rerere.autoupdate") {
			if err := cfg.SetConfig("rerere.autoupdate", "true"); err != nil {
				return false, err
			}
		}
		return false, nil
	}

	if configBool(cfg, declinedKey) || !interactive {
		return false, nil
	}

	ok, err := promptWithPause(pauser, confirm, "Enable git rerere to remember conflict resolutions?", true)
	if err != nil {
		return false, nil //nolint:nilerr // prompt cancel (Ctrl+C) should not error — treat as decline without persisting
	}
	if !ok {
		_ = cfg.SetConfig(declinedKey, "true")
		return false, nil
	}

	if err := cfg.SetConfig("rerere.enabled", "true"); err != nil {
		return false, err
	}
	if err := cfg.SetConfig("rerere.autoupdate", "true"); err != nil {
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

func configBool(cfg GitConfigurer, key string) bool {
	value, err := cfg.GetConfig(key)
	if err != nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(value), "true")
}
