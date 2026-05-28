// Package hooks orchestrates hook discovery, approval, and execution at the
// actions layer. The pure executor lives in internal/hooks; this package
// adds the interactive approval gate and the bridge to app.Context.
package hooks

import (
	"context"
	"fmt"
	"strings"

	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/config"
	corehooks "github.com/getstackit/stackit/internal/hooks"
	"github.com/getstackit/stackit/internal/output"
	"github.com/getstackit/stackit/internal/tui"
)

// PromptMessage produces the confirmation prompt shown when a hook needs
// first-time approval. Phase is rendered verbatim so users can tell which
// lifecycle slot a command would run in.
func PromptMessage(phase, hook string) string {
	return fmt.Sprintf("This repo wants to run %q at %s. Allow?", hook, phase)
}

// ResolveApproved loads the per-repo approval list for the phase, prompting
// the user for any unapproved commands. Empty input is handled cheaply: no
// config reads or prompts happen when commands is empty.
//
// Must run from the main goroutine — it may prompt interactively.
func ResolveApproved(ctx *app.Context, phase string, commands []string) ([]string, error) {
	out := ctx.Output

	// Filter empty/whitespace-only entries up front.
	filtered := commands[:0:0]
	for _, hook := range commands {
		if trimmed := strings.TrimSpace(hook); trimmed != "" {
			filtered = append(filtered, hook)
		}
	}
	if len(filtered) == 0 {
		return nil, nil
	}

	repoCfg, err := config.LoadConfig(ctx.RepoRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to load repo config: %w", err)
	}

	approved := make([]string, 0, len(filtered))
	for _, hook := range filtered {
		if repoCfg.IsHookApproved(phase, hook) {
			approved = append(approved, hook)
			continue
		}

		allow, promptErr := tui.PromptConfirm(PromptMessage(phase, hook), false)
		if promptErr != nil {
			out.Info("Skipping hook (prompt failed): %s", hook)
			continue
		}
		if !allow {
			out.Info("Skipping hook: %s", hook)
			continue
		}

		if err := repoCfg.AddApprovedHook(phase, hook); err != nil {
			out.Warn("Failed to save hook approval: %v", err)
		}
		approved = append(approved, hook)
	}

	return approved, nil
}

// RunOptions controls how a list of resolved hooks is executed.
type RunOptions struct {
	// Dir is the working directory for each hook process. Defaults to the
	// caller's cwd if empty.
	Dir string
	// Env is a list of NAME=value strings appended to os.Environ() for each
	// hook invocation. nil is allowed.
	Env []string
	// Blocking, when true, causes the first non-zero exit to abort the rest
	// and return the error to the caller. When false, errors are surfaced as
	// warnings on out and execution continues.
	Blocking bool
}

// Run executes a pre-resolved list of hook commands with the shared executor.
// On Blocking=true the first failure returns immediately; on Blocking=false
// failures are logged and the remaining hooks still run.
func Run(ctx context.Context, hookCmds []string, out output.Output, opts RunOptions) error {
	for _, hook := range hookCmds {
		out.Info("Running hook: %s", hook)
		if err := corehooks.Run(ctx, hook, opts.Dir, opts.Env, corehooks.DefaultTimeout); err != nil {
			if opts.Blocking {
				return fmt.Errorf("hook %q failed: %w", hook, err)
			}
			out.Warn("Hook failed: %s: %v", hook, err)
		}
	}
	return nil
}
