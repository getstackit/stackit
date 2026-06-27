package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/git"
)

// revParse returns the resolved SHA of a ref in the test repo.
func (s *TestShell) revParse(ref string) string {
	s.t.Helper()
	s.Git("rev-parse " + ref)
	return strings.TrimSpace(s.Output())
}

// recordPR stamps a PR number and state onto a branch's metadata. The squash
// scan is gated behind a recorded PR number, so detection tests must give the
// merged branch a PR to mirror real GitHub usage (a squash-merged branch always
// had a PR).
func (s *TestShell) recordPR(branch string, prNumber int, state, base string) {
	s.t.Helper()
	runner := git.NewRunnerWithPath(s.Dir(), nil)
	meta, err := runner.ReadMetadata(branch)
	require.NoError(s.t, err)
	num := prNumber
	st := state
	b := base
	meta = meta.WithPrInfo(&git.PrInfoPersistence{Number: &num, State: &st, Base: &b})
	require.NoError(s.t, runner.WriteMetadata(branch, meta))
}

// fileContent reads a working-tree file from the test repo.
func (s *TestShell) fileContent(name string) string {
	s.t.Helper()
	data, err := os.ReadFile(filepath.Join(s.scene.Dir, name))
	require.NoError(s.t, err, "failed to read %s", name)
	return string(data)
}

// TestSquashMergedParentConflictAbortPreservesChild locks in the escalation path
// from issue #1345.
//
// Setup: main -> parent (multi-commit) -> child. The parent is squash-merged onto
// main with a stale (OPEN) PR state, so detection must come from aggregate
// patch-id, not recorded MERGED metadata. A later trunk edit touches the same
// line the child touches, so when the child reparents onto trunk its own commit
// CONFLICTS.
//
// The invariant: after the conflict is aborted, the child must be restored to its
// original commit — its work intact, never reset to the squash-merged parent's
// stale tip, never left mid-rebase or detached. This is exactly the data loss the
// issue reported (a branch reset to a sibling's tip with its own commit orphaned).
func TestSquashMergedParentConflictAbortPreservesChild(t *testing.T) {
	t.Parallel()
	sh := NewTestShellInProcess(t)

	// main -> parent: two commits so the squash is genuinely multi-commit
	// (per-commit patch matching via git cherry cannot detect it).
	sh.WriteFile("shared.txt", "base\np2\n").
		Run("create parent -m 'parent c1'").
		OnBranch("parent")
	sh.WriteFile("notes.txt", "parent note\n").
		Git("commit -m 'parent c2'")

	// parent -> child: the child rewrites the shared line and adds a unique file.
	sh.WriteFile("shared.txt", "base\nCHILD\n").
		WriteFile("child-only.txt", "child work\n").
		Run("create child -m 'child commit'").
		OnBranch("child")

	childOrig := sh.revParse("child")

	// GitHub squash-merges parent: one combined commit lands on main carrying the
	// parent's aggregate diff. The PR state stays stale (OPEN), so detection must
	// fall through to the patch-id scan, which is gated behind a recorded PR number.
	sh.Checkout("main")
	parentStaleTip := sh.revParse("parent")
	sh.WriteFile("shared.txt", "base\np2\n").
		WriteFile("notes.txt", "parent note\n").
		Git("commit -m 'squash-merge parent (#1)'")
	sh.recordPR("parent", 1, "OPEN", "main")

	// A later trunk commit edits the same shared line the child rewrote, so the
	// child's replay onto trunk is guaranteed to conflict.
	sh.WriteFile("shared.txt", "base\nMAINEDIT\n").
		Git("commit -m 'trunk edit on shared line'")

	// Restack the child. Detection reparents it past the squash-merged parent onto
	// main, and replaying its commit conflicts on shared.txt.
	sh.Checkout("child").
		RunExpectError("restack --upstack").
		OutputContains("conflict")

	// Abort: must roll the child fully back to where it started. The conflict
	// workflow detaches HEAD (writing a "checkout: moving from" reflog entry),
	// which previously fooled the abort router into treating this restack conflict
	// as a failed absorb — that path never ran `git rebase --abort`, leaving the
	// repo stuck mid-rebase. Assert we took the standard rebase-abort path.
	sh.Run("abort --force").
		OutputContains("Aborting rebase").
		OutputNotContains("absorb")

	require.Equal(t, childOrig, sh.revParse("child"),
		"child must be restored to its original commit after aborting the conflicted restack")
	require.NotEqual(t, parentStaleTip, sh.revParse("child"),
		"child must never point at the squash-merged parent's stale tip")
	require.Equal(t, "base\nCHILD\n", sh.fileContent("shared.txt"),
		"child's own change must survive the aborted restack")
	require.Equal(t, "child work\n", sh.fileContent("child-only.txt"),
		"child's unique file must survive the aborted restack")

	// No rebase left in progress, and we are back on a real branch (not detached).
	sh.Git("status")
	require.NotContains(t, sh.Output(), "rebas", "no rebase should be in progress after abort")
	sh.OnBranch("child")

	sh.Git("status --porcelain")
	require.Empty(t, strings.TrimSpace(sh.Output()), "working tree should be clean after abort")
}

// TestRestackWritesReflogMessageOnReparent locks in the second half of the fix:
// branch ref moves during restack must be annotated in the reflog. Issue #1345
// noted that blank "(no message)" reflog entries made the incident impossible to
// trace. Here a squash-merged parent (stale PR state) causes the child to
// reparent cleanly onto trunk; the resulting ref move must carry a
// "stackit: restack" message instead of an empty one.
func TestRestackWritesReflogMessageOnReparent(t *testing.T) {
	t.Parallel()
	sh := NewTestShellInProcess(t)

	sh.WriteFile("shared.txt", "base\np2\n").
		Run("create parent -m 'parent c1'").
		OnBranch("parent")
	sh.WriteFile("notes.txt", "parent note\n").
		Git("commit -m 'parent c2'")

	// Child adds only a unique file so its replay onto trunk applies cleanly.
	sh.WriteFile("child-only.txt", "child work\n").
		Run("create child -m 'child commit'").
		OnBranch("child")

	// Squash-merge parent onto main. The PR state is still stale (OPEN), so
	// detection falls through to the aggregate patch-id scan, which is gated
	// behind a recorded PR number.
	sh.Checkout("main")
	sh.WriteFile("shared.txt", "base\np2\n").
		WriteFile("notes.txt", "parent note\n").
		Git("commit -m 'squash-merge parent (#1)'")
	sh.recordPR("parent", 1, "OPEN", "main")

	sh.Checkout("child").
		Run("restack --upstack")

	// The child reparented onto main; its ref move must be annotated.
	sh.Git("reflog show child")
	require.Contains(t, sh.Output(), "stackit: restack",
		"restack must annotate branch ref moves in the reflog, not leave blank entries")
	sh.ExpectBranchParent("child", "main")

	sh.Git("status --porcelain")
	require.Empty(t, strings.TrimSpace(sh.Output()), "working tree should be clean after restack")
}
