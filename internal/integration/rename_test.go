package integration

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRenameBasic(t *testing.T) {
	t.Parallel()
	sh := NewTestShellInProcess(t)

	sh.Write("a", "content a").Run("create branch-a -m 'feat: a'")
	sh.OnBranch("branch-a")

	sh.Run("rename branch-b")
	sh.OnBranch("branch-b")
	sh.HasBranches("main", "branch-b")
	sh.Run("info").OutputContains("Parent: main")
}

func TestRenameWithChildren(t *testing.T) {
	t.Parallel()
	sh := NewTestShellInProcess(t)

	sh.Write("a", "content a").Run("create branch-a -m 'feat: a'")
	sh.Write("b", "content b").Run("create branch-b -m 'feat: b'")
	sh.Checkout("branch-a")

	sh.Run("rename branch-a-new")
	sh.OnBranch("branch-a-new")

	sh.Checkout("branch-b")
	sh.Run("info").OutputContains("Parent: branch-a-new")
}

func TestRenameWithPR(t *testing.T) {
	t.Parallel()
	sh := NewTestShellInProcess(t)

	sh.Write("a", "content a").Run("create branch-a -m 'feat: a'")

	// Manually set PR metadata
	prNumber := 123
	meta := map[string]interface{}{
		"parentBranchName": "main",
		"prInfo": map[string]interface{}{
			"number": prNumber,
			"url":    "https://github.com/owner/repo/pull/123",
		},
	}
	jsonData, _ := json.Marshal(meta)

	// Create blob
	cmd := exec.Command("git", "hash-object", "-w", "--stdin")
	cmd.Dir = sh.Dir()
	cmd.Stdin = strings.NewReader(string(jsonData))
	shaBytes, err := cmd.Output()
	require.NoError(t, err)
	sha := strings.TrimSpace(string(shaBytes))

	// Update ref
	sh.Git(fmt.Sprintf("update-ref refs/stackit/metadata/branch-a %s", sha))

	// Attempt rename without force - should fail
	sh.RunExpectError("rename branch-a-new").OutputContains("associated with PR #123").OutputContains("Use --force to proceed")

	// Rename with force - should succeed and clear PR info
	sh.Run("rename branch-a-new --force")
	sh.OnBranch("branch-a-new")
	sh.Run("info").OutputNotContains("PR #123")
}
