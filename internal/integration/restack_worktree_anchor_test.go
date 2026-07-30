package integration

import (
	"encoding/json"
	"os"
	"path/filepath"
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
// end was trunk's tip.
//
// What this test actually proves: the anchor fast-forwards to trunk, and the
// child's recorded parent revision converges to it, on every restack. That is
// the bookkeeping half of the fix, and it is load-bearing — reverting it fails
// at the first two assertions in assertTracksTrunk.
//
// It does NOT demonstrate the data-loss half — trunk's own commits replayed
// back onto the branch, which .claude/rules/multiplayer-safety.md forbids
// (<trunk>..<branch> must equal exactly the branch's own commits). With plain
// sequential trunk commits, the branch's recorded revision always stays an
// ancestor of the branch, so the merge-base substitution this bug is about
// (restack_plan.go:199-223) is never reached: the fallback that would use the
// wrong base only fires once the recorded revision stops being an ancestor,
// which needs trunk history that has been rewritten (squash or rebase merge)
// rather than only appended to. Git's patch-id dedup also drops any replayed
// commit whose content already matches something reachable from the new base,
// so even a widened range is invisible until a rewrite changes a commit's
// patch-id. See the GitHub Merge Method Coverage section of
// .claude/rules/testing.md for the same requirement elsewhere: a test with only
// appended, unrewritten trunk commits cannot exercise this class of bug.
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

		// Sanity check that the branch still contains only its own commit. It
		// does not independently regression-cover the replay bug described
		// above — see the package doc comment.
		sh.CommitCount("main", "feature", 1)
	}

	// Trunk advances and we restack. Correct even before the fix: the anchor
	// revision really is where the branch is based.
	sh.Checkout("main").
		Commit("trunk1.txt", "chore: trunk commit 1").
		Run("restack --all-stacks")
	assertTracksTrunk("after first restack")

	// Trunk advances again, so a second restack exercises the anchor and its
	// child on a revision that isn't the one either was created against.
	sh.Checkout("main").
		Commit("trunk2.txt", "chore: trunk commit 2").
		Run("restack --all-stacks")
	assertTracksTrunk("after second restack")
}

// TestRestackWorktreeAnchorPreservesUncommittedChanges covers the regression
// from #1490: `worktree create` with no root branch checks out the anchor
// itself in the new worktree, and #1488 made the anchor reachable by restack
// for the first time (it used to be skipped by the landed check). Restacking
// fast-forwards the anchor and then unconditionally ran `git reset --hard` on
// its worktree to match, discarding any uncommitted edit sitting there with no
// prompt, no warning, and no recovery path.
func TestRestackWorktreeAnchorPreservesUncommittedChanges(t *testing.T) {
	t.Parallel()

	sh := NewTestShellInProcess(t)
	sh.SetWorktreeBasePath(t.TempDir())

	// No --root-branch: the new worktree checks out the anchor directly.
	sh.Run("worktree create my-wt")
	sh.Run("worktree open my-wt")
	worktreePath := strings.TrimSpace(sh.Output())

	// Edit a tracked file inside the worktree and leave it uncommitted — the
	// shape of work that tends to live in a freshly created worktree.
	const uncommitted = "uncommitted edit"
	sh.InWorktree(worktreePath).WriteFile("init_test.txt", uncommitted)

	// Trunk advances elsewhere, giving the anchor somewhere to fast-forward to.
	sh.Checkout("main").Commit("trunk1.txt", "chore: trunk commit 1")

	sh.Run("restack --all-stacks")

	anchorBranch := findWorktreeAnchor(t, sh)
	sh.Git("rev-parse main")
	trunkRev := strings.TrimSpace(sh.Output())
	sh.Git("rev-parse " + anchorBranch)
	require.Equal(t, trunkRev, strings.TrimSpace(sh.Output()),
		"anchor should still fast-forward to trunk even when its worktree is dirty")

	content, err := os.ReadFile(filepath.Join(worktreePath, "init_test.txt"))
	require.NoError(t, err)
	require.Equal(t, uncommitted, string(content),
		"restack must not discard uncommitted changes in the anchor's worktree")
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
