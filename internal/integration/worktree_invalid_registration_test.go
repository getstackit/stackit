package integration

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestInvalidWorktreeRegistrationFailsClosed covers the ownership guard's
// handling of a registration it cannot trust. Ownership is resolved from one
// registry listing; if that listing skipped a broken registration, the stack it
// was registered for would look unowned and branch-content mutations would run
// from a checkout that may not own it. The refusal must stay scoped to that one
// stack: an unrelated stack and trunk keep working.
func TestInvalidWorktreeRegistrationFailsClosed(t *testing.T) {
	t.Parallel()

	registrationJSON := func(t *testing.T, anchor string) string {
		t.Helper()
		data, err := json.Marshal(map[string]any{
			"name":        "broken-wt",
			"path":        t.TempDir(),
			"stackRoot":   anchor,
			"createdAt":   "2026-01-01T00:00:00Z",
			"mainRepoDir": t.TempDir(),
		})
		require.NoError(t, err)
		return string(data)
	}

	tests := []struct {
		name string
		// blob is the raw registration content stored under
		// refs/stackit/worktrees/broken.
		blob func(t *testing.T) string
		// reason is the part of the refusal that identifies what is wrong.
		reason string
	}{
		{
			name:   "unparseable metadata blob",
			blob:   func(*testing.T) string { return "{not json" },
			reason: "failed to unmarshal worktree metadata for broken",
		},
		{
			// The mismatched anchor names another real stack. It must neither
			// release "broken" nor claim "other".
			name:   "ref name and anchor disagree",
			blob:   func(t *testing.T) string { return registrationJSON(t, "other") },
			reason: "invalid worktree registration for stack broken: metadata anchor is other",
		},
		{
			// An empty anchor must not attach to the empty stack root that
			// trunk and untracked branches resolve to.
			name:   "empty anchor",
			blob:   func(t *testing.T) string { return registrationJSON(t, "") },
			reason: "invalid worktree registration for stack broken: metadata anchor is ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			sh := NewTestShellInProcess(t)

			sh.Write("broken.txt", "broken").Run("create broken -m 'feat: broken'")
			sh.Checkout("main")
			sh.Write("other.txt", "other").Run("create other -m 'feat: other'")
			sh.Checkout("main")

			writeRawWorktreeRegistration(t, sh, "broken", tt.blob(t))

			// The broken stack refuses and names the bad registration.
			sh.Checkout("broken")
			sh.Write("broken.txt", "amended").RunExpectError("modify").
				OutputContains("cannot determine worktree ownership for branch broken").
				OutputContains(tt.reason)
			sh.Git("reset --hard")

			// Delete's full-stack cleanup path resolves ownership separately
			// and must fail closed the same way.
			sh.Checkout("main")
			sh.RunExpectError("delete broken --force").
				OutputContains(tt.reason)

			// An unrelated stack is unaffected.
			sh.Checkout("other")
			sh.Write("other.txt", "amended").Run("modify")

			// Trunk and new stacks rooted on it are unaffected.
			sh.Checkout("main")
			sh.Write("fresh.txt", "fresh").Run("create fresh -m 'feat: fresh'").
				OnBranch("fresh")
		})
	}
}

func writeRawWorktreeRegistration(t *testing.T, sh *TestShell, refName, content string) {
	t.Helper()

	hashCmd := exec.Command("git", "hash-object", "-w", "--stdin")
	hashCmd.Dir = sh.Scene().Dir
	hashCmd.Stdin = strings.NewReader(content)
	shaBytes, err := hashCmd.Output()
	require.NoError(t, err, "write worktree metadata blob")

	sh.Git("update-ref refs/stackit/worktrees/" + refName + " " + strings.TrimSpace(string(shaBytes)))
}
