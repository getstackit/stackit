package integration

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLifecycleHooks_PreModifyBlocks(t *testing.T) {
	t.Parallel()
	sh := NewTestShellInProcess(t)

	sh.WriteFile("seed.txt", "seed").Run("create feature -m 'feat: seed'")

	writeProjectConfig(t, sh.Dir(), `hooks:
  pre-modify:
    - "false"
`)
	// Pre-approve so the hook runs without an interactive prompt.
	sh.Git("config --add stackit.hooks.approved.pre-modify false")

	sh.WriteFile("seed.txt", "updated")
	sh.RunExpectError("modify -a")
	require.Contains(t, sh.lastOutput, "Running hook: false", "hook should be invoked")
	require.Contains(t, sh.lastOutput, "hook \"false\" failed", "hook failure should be reported")
}

func TestLifecycleHooks_NoVerifyBypasses(t *testing.T) {
	t.Parallel()
	sh := NewTestShellInProcess(t)

	sh.WriteFile("seed.txt", "seed").Run("create feature -m 'feat: seed'")

	writeProjectConfig(t, sh.Dir(), `hooks:
  pre-modify:
    - "false"
`)
	sh.Git("config --add stackit.hooks.approved.pre-modify false")

	sh.WriteFile("seed.txt", "updated")
	sh.Run("--no-verify modify -a")
	require.NotContains(t, sh.lastOutput, "Running hook:", "no hook should run with --no-verify")
}

func TestLifecycleHooks_ZeroCostWhenUnconfigured(t *testing.T) {
	t.Parallel()
	sh := NewTestShellInProcess(t)

	// No .stackit.yaml hooks section at all.
	sh.WriteFile("seed.txt", "seed").Run("create feature -m 'feat: seed'")
	sh.WriteFile("seed.txt", "updated")
	sh.Run("modify -a")

	require.NotContains(t, sh.lastOutput, "Running hook:", "no hook lines should appear when nothing is configured")
}

func TestLifecycleHooks_PostHookFailureIsWarning(t *testing.T) {
	t.Parallel()
	sh := NewTestShellInProcess(t)

	sh.WriteFile("seed.txt", "seed").Run("create feature -m 'feat: seed'")

	writeProjectConfig(t, sh.Dir(), `hooks:
  post-modify:
    - "false"
`)
	sh.Git("config --add stackit.hooks.approved.post-modify false")

	sh.WriteFile("seed.txt", "updated")
	// Modify still succeeds; post-hook failure is surfaced as a warning, not blocking.
	sh.Run("modify -a")
	require.Contains(t, sh.lastOutput, "Hook failed: false", "post-hook failure should be warned")
}
