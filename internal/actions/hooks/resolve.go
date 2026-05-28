// Package hooks orchestrates hook discovery, approval, and execution at the
// actions layer. The pure executor lives in internal/hooks; this package
// adds the interactive approval gate and the runner facade.
package hooks

import (
	"context"
	"fmt"
	"strings"

	"github.com/getstackit/stackit/internal/actions/handler"
	"github.com/getstackit/stackit/internal/config"
	corehooks "github.com/getstackit/stackit/internal/hooks"
	"github.com/getstackit/stackit/internal/output"
)

// promptMessage produces the confirmation prompt shown when a hook needs
// first-time approval. Phase is rendered verbatim so users can tell which
// lifecycle slot a command would run in.
func promptMessage(phase, hook string) string {
	return fmt.Sprintf("This repo wants to run %q at %s. Allow?", hook, phase)
}

// ResolveRequest carries the explicit dependencies needed to gate a list of
// hook commands behind per-user approval. All fields are required except
// Output, which may be nil to silence informational messages.
type ResolveRequest struct {
	// Phase is the lifecycle phase the hooks belong to (e.g. "pre-modify").
	Phase string
	// Commands is the configured hook command list for the phase. Empty
	// strings are ignored.
	Commands []string
	// Config provides the approval read/write store. The same Configurer
	// already loaded by app bootstrap is expected here — no extra disk read.
	Config config.Configurer
	// Prompter is used to ask the user about previously-unseen commands.
	// Implementations decide whether the prompt defaults to yes or no.
	Prompter handler.PromptHandler
	// Output sinks user-facing skip/warn messages. nil is permitted.
	Output output.Output
}

// ResolveApproved filters Commands down to the list the user has approved for
// Phase, prompting for any unapproved entries. Empty input is handled
// cheaply: no config writes or prompts happen when commands is empty.
//
// Must run from the main goroutine — it may prompt interactively.
func ResolveApproved(req ResolveRequest) ([]string, error) {
	filtered := make([]string, 0, len(req.Commands))
	for _, hook := range req.Commands {
		if trimmed := strings.TrimSpace(hook); trimmed != "" {
			filtered = append(filtered, hook)
		}
	}
	if len(filtered) == 0 {
		return nil, nil
	}
	if req.Config == nil {
		return nil, fmt.Errorf("resolve hooks: config is required")
	}
	if req.Prompter == nil {
		return nil, fmt.Errorf("resolve hooks: prompter is required")
	}

	approved := make([]string, 0, len(filtered))
	for _, hook := range filtered {
		if req.Config.IsHookApproved(req.Phase, hook) {
			approved = append(approved, hook)
			continue
		}

		allow, promptErr := req.Prompter.PromptConfirm(promptMessage(req.Phase, hook))
		if promptErr != nil {
			info(req.Output, "Skipping hook (prompt failed): %s", hook)
			continue
		}
		if !allow {
			info(req.Output, "Skipping hook: %s", hook)
			continue
		}

		if err := req.Config.AddApprovedHook(req.Phase, hook); err != nil {
			warn(req.Output, "Failed to save hook approval: %v", err)
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
	// warnings on Output and execution continues.
	Blocking bool
	// Output sinks per-hook progress and warning messages. nil is permitted
	// but discouraged — failures become silent on the non-blocking path.
	Output output.Output
}

// Run executes a pre-resolved list of hook commands with the shared executor.
// On Blocking=true the first failure returns immediately; on Blocking=false
// failures are logged to opts.Output and the remaining hooks still run.
func Run(ctx context.Context, hookCmds []string, opts RunOptions) error {
	for _, hook := range hookCmds {
		info(opts.Output, "Running hook: %s", hook)
		if err := corehooks.Run(ctx, hook, opts.Dir, opts.Env, corehooks.DefaultTimeout); err != nil {
			if opts.Blocking {
				return fmt.Errorf("hook %q failed: %w", hook, err)
			}
			warn(opts.Output, "Hook failed: %s: %v", hook, err)
		}
	}
	return nil
}

func info(out output.Output, format string, args ...any) {
	if out == nil {
		return
	}
	out.Info(format, args...)
}

func warn(out output.Output, format string, args ...any) {
	if out == nil {
		return
	}
	out.Warn(format, args...)
}
