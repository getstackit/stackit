package common

import (
	"os"

	"stackit.dev/stackit/internal/actions"
	"stackit.dev/stackit/internal/output"
)

// HasShellIntegration checks if stackit shell integration is installed.
// The shell wrapper sets STACKIT_SHELL_INTEGRATION=1 when running commands.
func HasShellIntegration() bool {
	return os.Getenv("STACKIT_SHELL_INTEGRATION") == "1"
}

// EmitDirective emits shell directives or shows fallback tips.
// This should be called by CLI commands after receiving a result from an action.
func EmitDirective(out output.Output, directive *actions.ShellDirective) {
	if directive == nil || !directive.HasDirective() {
		return
	}

	// If shell integration is not available, show fallback tip instead
	if !HasShellIntegration() && directive.FallbackTip != "" {
		out.Tip("%s", directive.FallbackTip)
		return
	}

	// Emit the directives for the shell wrapper to process
	if directive.CDPath != "" {
		out.DirectiveCD(directive.CDPath)
	}
	if len(directive.RerunArgs) > 0 {
		out.DirectiveRerun(directive.RerunArgs...)
	}
}
