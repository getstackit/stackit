package integration

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestRestackWorktreeAnchorTracksTrunk covers a stack living in a managed
// worktree, which sits under a hidden worktree anchor.
//
// An anchor holds no commits of its own — it marks where trunk was when the
// worktree was created — so it is always an ancestor of trunk. That made the
// landed check skip it before it ever reached the fast-forward path, so the
// anchor never moved. Two things then drift with every trunk advance: the
// child's recorded parent revision (nothing invalidates it, because a skipped
// anchor never counted as a moved parent), and the rebase range used to restack
// the child, whose `upstream` end came from that stale revision while its `onto`
// end was trunk's tip. The widening range replays trunk's own commits back onto
// the branch, which .claude/rules/multiplayer-safety.md forbids: after a
// restack, <trunk>..<branch> must equal exactly the branch's own commits.
//
// Restacking twice is what exposes it. One restack is fine, because the anchor
// revision really is where the branch is based.
func TestRestackWorktreeAnchorTracksTrunk(t *testing.T) {
	t.Parallel()

	sh := NewTestShellInProcess(t)
	sh.SetWorktreeBasePath(t.TempDir())

	// A worktree-backed stack: `worktree create` inserts the hidden anchor.
	sh.Run("worktree create my-wt")
	sh.Run("worktree open my-wt")
	worktreePath := strings.TrimSpace(sh.Output())

	sh.InWorktree(worktreePath).
		WriteFile("feature.txt", "feature").
		Run("create feature -m 'feat: feature'")

	anchorBranch := findWorktreeAnchor(t, sh)

	assertTracksTrunk := func(stage string) {
		t.Helper()

		sh.Git("rev-parse main")
		trunkRev := strings.TrimSpace(sh.Output())

		sh.Git("rev-parse " + anchorBranch)
		require.Equal(t, trunkRev, strings.TrimSpace(sh.Output()),
			"%s: anchor must fast-forward to trunk, otherwise it pins a stale rebase base", stage)

		require.Equal(t, trunkRev, recordedParentRev(t, sh, "feature"),
			"%s: child's recorded parent revision must track trunk", stage)

		// The invariant that actually protects the branch's contents.
		sh.CommitCount("main", "feature", 1)
	}

	// Trunk advances and we restack. Correct even before the fix: the anchor
	// revision really is where the branch is based.
	sh.Checkout("main").
		Commit("trunk1.txt", "chore: trunk commit 1").
		Run("restack --all-stacks")
	assertTracksTrunk("after first restack")

	// Trunk advances again. This is where the stale anchor used to widen the
	// replay range to cover every trunk commit since the worktree was created.
	sh.Checkout("main").
		Commit("trunk2.txt", "chore: trunk commit 2").
		Run("restack --all-stacks")
	assertTracksTrunk("after second restack")
}

// findWorktreeAnchor returns the name of the hidden worktree-anchor branch.
func findWorktreeAnchor(t *testing.T, sh *TestShell) string {
	t.Helper()

	sh.Git("for-each-ref --format=%(refname:short) refs/heads/")
	for _, branch := range strings.Fields(sh.Output()) {
		if sh.isWorktreeAnchorBranch(branch) {
			return branch
		}
	}

	require.FailNow(t, "no worktree anchor branch found")
	return ""
}

// recordedParentRev reads the parent revision stackit has recorded for a branch.
func recordedParentRev(t *testing.T, sh *TestShell, branch string) string {
	t.Helper()

	sh.Git("rev-parse refs/stackit/metadata/" + branch)
	sh.Git("cat-file -p " + strings.TrimSpace(sh.Output()))

	var meta struct {
		ParentBranchRevision string `json:"parentBranchRevision"`
	}
	require.NoError(t, json.Unmarshal([]byte(sh.Output()), &meta))
	return meta.ParentBranchRevision
}
