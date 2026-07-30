package engine_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

// TestRestackBranchWorktreeAnchorChildUsesTrunkForMergeBaseFallback covers the
// sibling of the bug fixed in #1488 on PlanRestack: restackBranch (the raw,
// non-plan path engine.RestackBranches uses -- reached in production when a
// branch conflicts mid-restack via EnterConflictWorkflow, see
// internal/actions/common.go) computed its RESILIENCY merge-base fallback
// against the worktree anchor's own (possibly stale) name, rather than the
// already-trunk-substituted rebaseOnto value used for the other end of the
// rebase. If the anchor's ref lags behind where the branch actually diverged,
// merge-basing against it instead of trunk walks back past the branch's own
// fork point and pulls trunk's own commits into the replay range -- which
// .claude/rules/multiplayer-safety.md forbids (<trunk>..<branch> must equal
// exactly the branch's own commits).
//
// A too-early upstream boundary is normally invisible: `git rebase --onto`
// silently no-ops a replayed commit whose change is already an ancestor of
// the new base. To make the widened range observable, trunk's later history
// *deletes* the file the wrongly-replayed commit added. Replaying that old
// "add" is then a real, non-ancestor change that resurrects a file trunk
// deliberately removed, instead of a harmless already-applied no-op.
func TestRestackBranchWorktreeAnchorChildUsesTrunkForMergeBaseFallback(t *testing.T) {
	t.Parallel()

	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

	// main: init -> A (adds shared.txt) -- the anchor's real creation point.
	s.CheckoutQuiet("main")
	writeFile(t, s, "shared.txt", "original")
	s.RunGit("add", "shared.txt").Commit("chore: add shared.txt")

	s.CreateBranchFromQuiet("anchor", "main").Rebuild()
	anchorBranch := s.Engine.GetBranch("anchor")
	require.NoError(t, s.Engine.SetBranchType(anchorBranch, git.BranchTypeWorktreeAnchor))
	s.TrackBranch("anchor", "main")

	s.CreateBranchFromQuiet("feature", "anchor").Rebuild()
	s.CommitChange("feature.txt", "feat: feature")
	s.TrackBranch("feature", "anchor")

	// An unrelated branch, off main before it advances further. Only its SHA
	// is used, to give the RESILIENCY fallback below a recorded revision
	// that is provably not an ancestor of feature.
	s.CreateBranchFromQuiet("other", "main").Rebuild()
	s.CommitChange("other.txt", "chore: other")
	otherRev := revParse(t, s, "other")

	rootRev := revParse(t, s, "main~1") // the commit before A, i.e. before shared.txt existed

	// main advances further, well ahead of where the anchor was created --
	// and deletes the very file A introduced. Trunk's current tip therefore
	// no longer contains shared.txt at all.
	s.CheckoutQuiet("main")
	removeFile(t, s, "shared.txt")
	s.RunGit("rm", "shared.txt")
	s.Commit("chore: remove shared.txt")
	s.Rebuild()

	// Corrupt the anchor's ref back to BEFORE its real creation point --
	// simulating the anchor desyncing from where the branch actually forked.
	// This is the "stale anchor" restackBranch's rebaseOnto substitution
	// exists to route around; the question this test asks is whether the
	// merge-base FALLBACK also routes around it, or falls back to the
	// anchor's raw (too-early) name.
	forceUpdateRef(t, s, "refs/heads/anchor", rootRev)

	// Corrupt feature's recorded parent revision to a commit that is not an
	// ancestor of feature at all, forcing the IsAncestor check in
	// restackBranch to fail and fall back to a merge-base recomputation --
	// the exact code path this fix changes.
	setRecordedParentRevision(t, s, "feature", otherRev)
	s.Rebuild() // invalidate the engine's in-memory metadata cache so the corruption above is visible

	// This mirrors EnterConflictWorkflow's call exactly: a single-branch,
	// non-plan restack (internal/actions/common.go calls
	// ctx.Engine.RestackBranches(ctx, engine.BranchesOf(conflictBranch))).
	result, err := s.Engine.RestackBranches(context.Background(), engine.BranchesOf(s.Engine.GetBranch("feature")))
	require.NoError(t, err)
	require.Equal(t, engine.RestackDone, result.Results["feature"].Result)

	s.Rebuild()
	require.Equal(t, 1, commitCountBetween(t, s, "main", "feature"),
		"main..feature must contain exactly feature's own commit; a wider count means trunk's own commit (A) was replayed onto feature")

	_, err = s.Scene.Repo.RunGitCommandAndGetOutput("show", "feature:shared.txt")
	require.Error(t, err,
		"shared.txt must stay deleted on feature: a widened replay range would resurrect trunk's own "+
			"commit that (re-)added it, even though trunk deliberately deleted it afterward")
}

// writeFile creates or overwrites a file in the scenario's working tree.
func writeFile(t *testing.T, s *scenario.Scenario, name, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(s.Scene.Dir, name), []byte(content), 0o644))
}

// removeFile deletes a file from the scenario's working tree.
func removeFile(t *testing.T, s *scenario.Scenario, name string) {
	t.Helper()
	require.NoError(t, os.Remove(filepath.Join(s.Scene.Dir, name)))
}

// commitCountBetween returns the number of commits reachable from `to` but
// not from `from` (git rev-list --count from..to) -- the invariant
// .claude/rules/multiplayer-safety.md requires for <trunk>..<branch>.
func commitCountBetween(t *testing.T, s *scenario.Scenario, from, to string) int {
	t.Helper()
	out, err := s.Scene.Repo.RunGitCommandAndGetOutput("rev-list", "--count", from+".."+to)
	require.NoError(t, err)
	n, err := strconv.Atoi(strings.TrimSpace(out))
	require.NoError(t, err)
	return n
}

// revParse resolves a revision expression to its SHA.
func revParse(t *testing.T, s *scenario.Scenario, rev string) string {
	t.Helper()
	out, err := s.Scene.Repo.RunGitCommandAndGetOutput("rev-parse", rev)
	require.NoError(t, err)
	return strings.TrimSpace(out)
}

// forceUpdateRef points a ref directly at a SHA, bypassing normal branch
// operations -- used to simulate a ref that desynced from where stackit
// expects it to be.
func forceUpdateRef(t *testing.T, s *scenario.Scenario, ref, sha string) {
	t.Helper()
	_, err := s.Scene.Repo.RunGitCommandAndGetOutput("update-ref", ref, sha)
	require.NoError(t, err)
}

// setRecordedParentRevision overwrites a branch's recorded
// parentBranchRevision in its stackit metadata blob directly via git
// plumbing, without going through the engine (which always computes a
// genuinely-ancestor value). This simulates metadata that was corrupted or
// written by a separate bug rather than reflecting the branch's real base.
func setRecordedParentRevision(t *testing.T, s *scenario.Scenario, branch, rev string) {
	t.Helper()

	refName := git.MetadataRefPrefix + branch
	blobSHA, err := s.Scene.Repo.RunGitCommandAndGetOutput("show-ref", "-s", refName)
	require.NoError(t, err)

	blob, err := s.Scene.Repo.RunGitCommandAndGetOutput("cat-file", "-p", strings.TrimSpace(blobSHA))
	require.NoError(t, err)

	var meta map[string]any
	require.NoError(t, json.Unmarshal([]byte(blob), &meta))
	meta["parentBranchRevision"] = rev

	data, err := json.Marshal(meta)
	require.NoError(t, err)

	cmd := exec.Command("git", "hash-object", "-w", "--stdin")
	cmd.Dir = s.Scene.Dir
	cmd.Stdin = strings.NewReader(string(data))
	newBlobSHA, err := cmd.Output()
	require.NoError(t, err)

	forceUpdateRef(t, s, refName, strings.TrimSpace(string(newBlobSHA)))
}
