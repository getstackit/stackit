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
// end was trunk's tip. The widening range replays trunk's own commits back onto
// the branch, which .claude/rules/multiplayer-safety.md forbids: after a
// restack, <trunk>..<branch> must equal exactly the branch's own commits.
//
// The stack goes two levels deep (anchor -> feature -> feature2) to broaden
// coverage of the newly-reachable anchor path beyond a single child.
//
// The commit-count assertions below are not, by themselves, a regression
// test for the replay bug -- verified by reverting internal/engine/restack_plan.go
// to its pre-fix version and running this test with only those assertions
// left in: it still passes, at both stack depths. A reverted build treats
// the anchor as already landed (it is always an ancestor of trunk) and skips
// it before ever reaching the fast-forward path, so it never moves; every
// descendant then keeps comparing itself to that same stale anchor tip and
// also gets skipped as "up to date". Nothing gets replayed, so the commit
// counts stay right by accident. What actually catches the regression is the
// bookkeeping asserted above -- the anchor failing to fast-forward to trunk,
// and the child's recorded parent revision failing to track it -- confirmed
// by the same revert, which fails there instead. The commit counts stay as
// an integrity check on the stack's contents once a restack has genuinely
// happened, not as the primary regression signal.
func TestRestackWorktreeAnchorTracksTrunk(t *testing.T) {
	t.Parallel()

	sh := NewTestShellInProcess(t)
	sh.SetWorktreeBasePath(t.TempDir())

	// A worktree-backed stack: `worktree create` inserts the hidden anchor.
	sh.Run("worktree create my-wt")
	sh.Run("worktree open my-wt")
	worktreePath := strings.TrimSpace(sh.Output())

	wt := sh.InWorktree(worktreePath)
	wt.WriteFile("feature.txt", "feature").
		Run("create feature -m 'feat: feature'")
	wt.WriteFile("feature2.txt", "feature2").
		Run("create feature2 -m 'feat: feature2'")

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

		// Integrity check once a restack has actually happened -- see the
		// bookkeeping asserts above for what actually catches the regression.
		sh.CommitCount("main", "feature", 1)
		sh.CommitCount("main", "feature2", 2)
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

// TestRestackWorktreeAnchorPreservesUncommittedChanges covers the regression
// from #1490: `worktree create` with no root branch checks out the anchor
// itself in the new worktree, and #1488 made the anchor reachable by restack
// for the first time (it used to be skipped by the landed check). Restacking
// fast-forwards the anchor and then unconditionally ran `git reset --hard` on
// its worktree to match, discarding any uncommitted edit sitting there with no
// prompt, no warning, and no recovery path.
//
// Restack now skips the whole stack while its worktree is dirty, so the anchor
// stays put rather than moving to trunk without its working directory following.
// See TestRestackWorktreeAnchorSkipsDirtyStack for that half.
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

	content, err := os.ReadFile(filepath.Join(worktreePath, "init_test.txt"))
	require.NoError(t, err)
	require.Equal(t, uncommitted, string(content),
		"restack must not discard uncommitted changes in the anchor's worktree")
}

// TestRestackWorktreeAnchorSkipsDirtyStack covers the other half of #1490:
// suppressing only the working-directory reset would still fast-forward the
// anchor's ref, leaving its worktree holding the old trunk content. `git status`
// there then reports trunk's new files as deleted, so a `git add -A` in that
// worktree would commit a revert of trunk. Restack instead skips the stack
// entirely — visibly — until the worktree is clean, matching what sync already
// does (internal/actions/sync/sync.go).
func TestRestackWorktreeAnchorSkipsDirtyStack(t *testing.T) {
	t.Parallel()

	sh := NewTestShellInProcess(t)
	sh.SetWorktreeBasePath(t.TempDir())

	sh.Run("worktree create my-wt")
	sh.Run("worktree open my-wt")
	worktreePath := strings.TrimSpace(sh.Output())

	anchorBranch := findWorktreeAnchor(t, sh)
	sh.Git("rev-parse " + anchorBranch)
	anchorRevBefore := strings.TrimSpace(sh.Output())

	sh.InWorktree(worktreePath).WriteFile("dirty.txt", "uncommitted work")

	sh.Checkout("main").
		Commit("trunk1.txt", "chore: trunk commit 1").
		Run("restack --all-stacks").
		OutputContains("worktree has uncommitted changes")

	// The anchor is left where it was rather than moving to trunk without its
	// working directory.
	sh.Git("rev-parse " + anchorBranch)
	require.Equal(t, anchorRevBefore, strings.TrimSpace(sh.Output()),
		"dirty anchor must not be fast-forwarded while its worktree has uncommitted changes")

	// And the uncommitted work is still there.
	content, err := os.ReadFile(filepath.Join(worktreePath, "dirty.txt"))
	require.NoError(t, err)
	require.Equal(t, "uncommitted work", string(content))
}

// TestRestackUpstackConvergesForWorktreeAnchorChild covers #1491: `restack
// --upstack` (and `--only`) on a child of a worktree anchor used to rewrite
// the branch's commit on every run and never reach an up-to-date state, even
// with no further trunk movement between runs. Contents stayed correct (no
// replay), but each run minted a fresh SHA, which force-pushes on the next
// submit, re-triggers CI, and detaches existing review threads from their
// commits.
//
// Unlike TestRestackWorktreeAnchorTracksTrunk, this scope deliberately
// excludes the anchor from the restack set (`--upstack` starts at feature),
// so the anchor's ref never advances and the child's recorded parent
// revision can never catch up to it. That keeps the branch "planned" forever
// even once it is already correctly based, so what's under test is that the
// conflict-free replay path stops minting a new commit once nothing is left
// to replay.
func TestRestackUpstackConvergesForWorktreeAnchorChild(t *testing.T) {
	t.Parallel()

	sh := NewTestShellInProcess(t)
	sh.SetWorktreeBasePath(t.TempDir())

	sh.Run("worktree create my-wt")
	sh.Run("worktree open my-wt")
	worktreePath := strings.TrimSpace(sh.Output())

	sh.InWorktree(worktreePath).
		WriteFile("feature.txt", "feature").
		Run("create feature -m 'feat: feature'")

	// One trunk advance, then a real restack: feature actually needs to move.
	sh.Checkout("main").
		Commit("trunk1.txt", "chore: trunk commit 1").
		Run("restack --branch feature --upstack")

	sh.Git("rev-parse feature")
	firstRev := strings.TrimSpace(sh.Output())

	// No further trunk movement. Repeated restacks scoped to exclude the
	// anchor must converge: same SHA, reported as already up to date.
	for i := 2; i <= 3; i++ {
		sh.Run("restack --branch feature --upstack").
			OutputContains("up to date")

		sh.Git("rev-parse feature")
		require.Equal(t, firstRev, strings.TrimSpace(sh.Output()),
			"run %d: feature's SHA must stay stable once it is already correctly based", i)
	}

	sh.CommitCount("main", "feature", 1)
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
