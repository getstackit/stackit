package common

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/getstackit/stackit/internal/actions/handler"
	"github.com/getstackit/stackit/internal/actions/hooks"
	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/config"
	"github.com/getstackit/stackit/internal/tui"
)

// PhasePrefix identifies whether a hook fires before or after the command.
type PhasePrefix string

const (
	PhasePre  PhasePrefix = "pre"
	PhasePost PhasePrefix = "post"
)

// commandPath returns the kebab-cased cobra command path with the leading
// "stackit" segment stripped, e.g. "modify" or "worktree-create". Returns an
// empty string for the root command.
func commandPath(cmd *cobra.Command) string {
	parts := strings.Fields(cmd.CommandPath())
	if len(parts) > 0 && parts[0] == "stackit" {
		parts = parts[1:]
	}
	return strings.Join(parts, "-")
}

// CommandPhase returns the lookup key under hooks: in .stackit.yaml for the
// given cobra command and phase, e.g. "pre-modify" or "post-worktree-create".
func CommandPhase(cmd *cobra.Command, phase PhasePrefix) string {
	path := commandPath(cmd)
	if path == "" {
		return string(phase)
	}
	return string(phase) + "-" + path
}

// RunCommandHooks fires the configured hooks for the given lifecycle phase.
//
// Zero-cost when absent: the only work performed when no hook is configured
// for this command + phase is a slice-length check against the in-memory
// project config that was already loaded during context construction.
//
// Returns the hook's error verbatim for pre-hooks (so the caller can abort
// the command). For post-hooks, errors are surfaced as warnings on
// ctx.Output and nil is returned.
func RunCommandHooks(ctx *app.Context, cmd *cobra.Command, phase PhasePrefix) error {
	if !ctx.Verify {
		return nil
	}
	projectCfg := projectConfigFrom(ctx)
	if projectCfg == nil {
		return nil
	}

	phaseKey := CommandPhase(cmd, phase)
	if phaseKey == config.PhasePostWorktreeCreate {
		// post-worktree-create is the legacy worktree hook. Worktree create/attach
		// run it explicitly inside the target worktree; the generic command
		// lifecycle runner would run the same hook again from the original repo.
		return nil
	}
	hookCmds := projectCfg.Hooks.For(phaseKey)
	if len(hookCmds) == 0 {
		return nil
	}

	approved, err := hooks.ResolveApproved(hooks.ResolveRequest{
		Phase:    phaseKey,
		Commands: hookCmds,
		Config:   ctx.Config,
		Prompter: defaultPrompter{},
		Output:   ctx.Output,
		Required: phase == PhasePre,
	})
	if err != nil {
		return fmt.Errorf("resolve hooks for %s: %w", phaseKey, err)
	}
	if len(approved) == 0 {
		return nil
	}

	return hooks.Run(ctx, approved, hooks.RunOptions{
		Dir:      ctx.RepoRoot,
		Env:      hookEnv(ctx, cmd, phaseKey),
		Blocking: phase == PhasePre,
		Output:   ctx.Output,
	})
}

func projectConfigFrom(ctx *app.Context) *config.ProjectConfig {
	if ctx.Config == nil {
		return nil
	}
	return ctx.Config.ProjectConfig()
}

// defaultPrompter is a handler.PromptHandler that delegates to the shared
// TUI confirmation prompt with a default-no answer. Default-no protects users
// who hit Enter on an unexpected prompt from executing arbitrary commands.
type defaultPrompter struct{}

func (defaultPrompter) PromptConfirm(message string) (bool, error) {
	return tui.PromptConfirm(message, false)
}

// Ensure defaultPrompter satisfies the interface at compile time.
var _ handler.PromptHandler = defaultPrompter{}

func hookEnv(ctx *app.Context, cmd *cobra.Command, phaseKey string) []string {
	env := []string{
		"STACKIT_HOOK_PHASE=" + phaseKey,
		"STACKIT_COMMAND=" + cmd.Name(),
		"STACKIT_COMMAND_PATH=" + commandPath(cmd),
	}
	if ctx.Engine != nil {
		if cur := ctx.Engine.CurrentBranch(); cur != nil {
			env = append(env, "STACKIT_BRANCH="+cur.GetName())
			if parent := cur.GetParent(); parent != nil {
				env = append(env, "STACKIT_PARENT="+parent.GetName())
			}
			if pr, err := cur.GetPrInfo(); err == nil && pr != nil {
				if n := pr.Number(); n != nil {
					env = append(env, fmt.Sprintf("STACKIT_PR_NUMBER=%d", *n))
				}
				if s := pr.State(); s != "" {
					env = append(env, "STACKIT_PR_STATE="+string(s))
				}
				env = append(env, fmt.Sprintf("STACKIT_PR_DRAFT=%t", pr.IsDraft()))
			}
		}
	}
	return env
}
