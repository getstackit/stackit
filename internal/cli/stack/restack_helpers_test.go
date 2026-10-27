package stack_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// createStackitBranch runs `stackit create <branch> -m <message>` in-process.
func createStackitBranch(t *testing.T, dir, branch, message string) {
	t.Helper()
	runCliCommandSuccess(t, dir, "create", branch, "-m", message)
}

// requireInfoParent asserts that `stackit info` on the current branch reports
// want as its parent. It matches the whole "Parent:" line so the assertion
// cannot be satisfied by the branch's own name or by want appearing elsewhere.
func requireInfoParent(t *testing.T, dir, want string) {
	t.Helper()
	output := runCliCommandSuccess(t, dir, "info")
	for line := range strings.SplitSeq(output, "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "Parent:"); ok {
			require.Equal(t, want, strings.TrimSpace(rest), "unexpected parent in info output:\n%s", output)
			return
		}
	}
	require.Failf(t, "info output has no Parent line", "want parent %q, got:\n%s", want, output)
}
