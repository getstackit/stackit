package integration

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/actions"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/handlers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

// This is the scenario 2 coverage matrix: restacking a child onto trunk after
// its parent's changes LANDED on origin/main and were synced. It exercises every
// GitHub merge method (merge commit, squash, rebase) for a parent with one and
// with multiple commits, for both a stackit user (parent carries a MERGED PR)
// and a non-stackit collaborator (parent carries zero stackit PR metadata).
//
// The invariant in all cases: the child reparents onto trunk and keeps ONLY its
// own commit — the parent's landed commits must never replay into the child. The
// trunk is modeled as synced (origin/main advanced via SyncRemoteTrunkToLocal),
// which is what distinguishes this from scenario 1 (un-pushed trunk, rejected by
// the guard).
//
// Detection coverage per strategy:
//   - merge commit: parent tip is reachable from trunk → ancestry (IsMerged).
//   - rebase merge: commits replayed with new SHAs but same patch-ids → cherry.
//   - squash (single commit): the lone commit patch-matches the squash → cherry.
//   - squash (multi commit): only the aggregate patch-id scan (no PR) or the
//     MERGED PR metadata (stackit user) can prove it landed.

type matrixMergeStrategy string

const (
	matrixMergeCommit matrixMergeStrategy = "merge-commit"
	matrixSquash      matrixMergeStrategy = "squash"
	matrixRebase      matrixMergeStrategy = "rebase"
)

func TestRestackOnSyncedTrunkMatrix(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		commits     []string // successive contents of the parent's "shared" file
		strategy    matrixMergeStrategy
		stackitUser bool
	}{
		{"merge-commit/single/stackit", []string{"v1"}, matrixMergeCommit, true},
		{"merge-commit/single/non-stackit", []string{"v1"}, matrixMergeCommit, false},
		{"merge-commit/multi/stackit", []string{"v1", "v1\nv2"}, matrixMergeCommit, true},
		{"merge-commit/multi/non-stackit", []string{"v1", "v1\nv2"}, matrixMergeCommit, false},
		{"squash/single/stackit", []string{"v1"}, matrixSquash, true},
		{"squash/single/non-stackit", []string{"v1"}, matrixSquash, false},
		{"squash/multi/stackit", []string{"v1", "v1\nv2"}, matrixSquash, true},
		{"squash/multi/non-stackit", []string{"v1", "v1\nv2"}, matrixSquash, false},
		{"rebase/single/stackit", []string{"v1"}, matrixRebase, true},
		{"rebase/single/non-stackit", []string{"v1"}, matrixRebase, false},
		{"rebase/multi/stackit", []string{"v1", "v1\nv2"}, matrixRebase, true},
		{"rebase/multi/non-stackit", []string{"v1", "v1\nv2"}, matrixRebase, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			sh := scenario.NewRemoteScenario(t)
			disableCommitSigning(t, sh)

			// Parent branch with the requested commit history.
			sh.CreateBranch("parent")
			for _, content := range tc.commits {
				sh.CommitChange("shared", content)
			}
			sh.TrackBranch("parent", "main")

			// Child stacked on parent. It touches a unique file so its replay onto
			// trunk never conflicts — isolating "did I inherit the parent's commits".
			sh.CreateBranch("child").
				CommitChange("child-only", "my work").
				TrackBranch("child", "parent")

			mainName := sh.Engine.Trunk().GetName()
			parentSHAs := branchCommitSHAs(t, sh, "parent")

			// Land the parent's changes on trunk via the chosen merge method.
			sh.Checkout("main")
			switch tc.strategy {
			case matrixMergeCommit:
				require.NoError(t, sh.Scene.Repo.RunGitCommand("merge", "--no-ff", "-m", "Merge parent (#1)", "parent"))
			case matrixSquash:
				sh.CommitChange("shared", tc.commits[len(tc.commits)-1])
			case matrixRebase:
				for _, c := range parentSHAs {
					require.NoError(t, sh.Scene.Repo.RunGitCommand("cherry-pick", c))
				}
			}
			sh.Rebuild()

			// The merge landed on the real trunk and was fetched: origin/main now
			// carries it, so the restack guard sees synced trunk (not un-pushed).
			sh.SyncRemoteTrunkToLocal()

			// A stackit user's parent carries a MERGED PR; a non-stackit
			// collaborator's parent carries no stackit PR metadata at all.
			if tc.stackitUser {
				markPrMerged(t, sh, "parent", 1, mainName)
			}

			sh.Checkout("child")
			plan, err := actions.PlanRestack(sh.Context, actions.RestackOptions{
				BranchName: "child",
				Scope:      engine.StackRangeFull(),
			})
			require.NoError(t, err)
			require.NoError(t, actions.RestackAction(sh.Context, plan, &handlers.NullRestackHandler{}))
			sh.Rebuild()

			require.Equal(t, mainName, sh.Engine.GetBranch("child").GetParent().GetName(),
				"child should reparent to trunk past the landed parent")
			count, err := sh.Scene.Repo.GetCommitCount("main", "child")
			require.NoError(t, err)
			require.Equal(t, 1, count,
				"child must contain only its own commit, never the parent's landed commits")
			requireCleanWorkingTree(t, sh)
		})
	}
}

// branchCommitSHAs returns the SHAs of the commits on branch that are not on
// main, oldest first.
func branchCommitSHAs(t *testing.T, sh *scenario.Scenario, branch string) []string {
	t.Helper()
	out, err := sh.Scene.Repo.RunGitCommandAndGetOutput("rev-list", "--reverse", "main.."+branch)
	require.NoError(t, err)
	return strings.Fields(out)
}
