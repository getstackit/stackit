package actions

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/git"
)

// remoteMetadata is a remoteMetadataLookup for tests, standing in for the
// engine's cache of metadata refs fetched from the remote.
type remoteMetadata map[string]*git.Meta

func (m remoteMetadata) Get(branch string) *git.Meta { return m[branch] }

// localBranches is a localBranchSet for tests, standing in for the engine's
// cached list of every branch that exists in refs/heads.
type localBranches []string

func (l localBranches) Contains(name string) bool {
	for _, b := range l {
		if b == name {
			return true
		}
	}
	return false
}

func newTestTargets(parents map[string]string, branches []string) *syncTargets {
	targets := newSyncTargets(branches[len(branches)-1])
	targets.branches = branches
	for branch, parent := range parents {
		targets.parentByBranch[branch] = parent
	}
	return targets
}

// reanchorPastLanded decides which parent a fetched branch is recorded against
// when ancestors have disappeared from the remote. The walk has to skip a run
// of landed ancestors rather than stopping at the first one, and has to leave
// alone a parent that is gone from the remote but still checked out locally.
func TestReanchorPastLanded(t *testing.T) {
	t.Parallel()

	t.Run("re-anchors onto trunk when the whole downstack has landed", func(t *testing.T) {
		t.Parallel()
		targets := newTestTargets(map[string]string{
			"a": "main",
			"b": "a",
			"c": "b",
		}, []string{"a", "b", "c"})

		reanchored := targets.reanchorPastLanded(localBranches(nil), []string{"a", "b"}, "main")

		require.Equal(t, []string{"c"}, targets.branches, "landed branches drop out of the sync set")
		require.Len(t, reanchored, 1)
		require.Equal(t, "c", reanchored["c"].Branch)
		require.Equal(t, "b", reanchored["c"].LandedParent)
		require.Equal(t, "main", reanchored["c"].NewParent)
		require.Equal(t, "main", targets.parentByBranch["c"])
	})

	t.Run("re-anchors onto the nearest ancestor that survives", func(t *testing.T) {
		t.Parallel()
		targets := newTestTargets(map[string]string{
			"a": "main",
			"b": "a",
			"c": "b",
		}, []string{"a", "b", "c"})

		reanchored := targets.reanchorPastLanded(localBranches(nil), []string{"b"}, "main")

		require.Equal(t, []string{"a", "c"}, targets.branches)
		require.Len(t, reanchored, 1)
		require.Equal(t, "a", reanchored["c"].NewParent, "a is still on the remote, so c belongs under it")
		require.Equal(t, "a", targets.parentByBranch["c"])
	})

	t.Run("leaves a parent that still exists locally as the parent", func(t *testing.T) {
		t.Parallel()
		targets := newTestTargets(map[string]string{
			"a": "main",
			"b": "a",
		}, []string{"a", "b"})

		reanchored := targets.reanchorPastLanded(localBranches{"main", "a", "b"}, []string{"a"}, "main")

		require.Equal(t, []string{"b"}, targets.branches, "a cannot be fetched, so it leaves the sync set either way")
		require.Empty(t, reanchored, "restack reparents past a landed local parent on its own")
		require.Equal(t, "a", targets.parentByBranch["b"])
	})

	t.Run("stops at trunk when the ancestry chain is incomplete", func(t *testing.T) {
		t.Parallel()
		targets := newTestTargets(map[string]string{"b": "a"}, []string{"b"})

		reanchored := targets.reanchorPastLanded(localBranches(nil), []string{"a"}, "main")

		require.Len(t, reanchored, 1)
		require.Equal(t, "main", reanchored["b"].NewParent)
	})

	t.Run("is a no-op when every branch is still on the remote", func(t *testing.T) {
		t.Parallel()
		targets := newTestTargets(map[string]string{"a": "main", "b": "a"}, []string{"a", "b"})

		reanchored := targets.reanchorPastLanded(localBranches(nil), nil, "main")

		require.Equal(t, []string{"a", "b"}, targets.branches)
		require.Empty(t, reanchored)
	})

	// Two ancestors landing at different times leaves two substitutions with
	// different new parents. Nothing downstream may assume they share one.
	t.Run("records each substitution when two ancestors have landed", func(t *testing.T) {
		t.Parallel()
		targets := newTestTargets(map[string]string{
			"a": "main",
			"b": "a",
			"c": "b",
			"d": "c",
		}, []string{"a", "b", "c", "d"})

		reanchored := targets.reanchorPastLanded(localBranches(nil), []string{"a", "c"}, "main")

		require.Equal(t, []string{"b", "d"}, targets.branches)
		require.Len(t, reanchored, 2)
		require.Equal(t, "main", reanchored["b"].NewParent)
		require.Equal(t, "b", reanchored["d"].NewParent, "d belongs under the surviving b, not trunk")
		require.Equal(t, []string{"b", "d"},
			[]string{targets.reanchoredInSyncOrder(reanchored)[0].Branch, targets.reanchoredInSyncOrder(reanchored)[1].Branch},
			"the report is ordered trunk-first, not by map iteration")
	})

	// The anchor is what lets a later restack replay only the branch's own
	// commits, so it has to be carried into the record the sync loop reads.
	t.Run("carries the divergence anchor into the record", func(t *testing.T) {
		t.Parallel()
		targets := newTestTargets(map[string]string{"a": "main", "b": "a"}, []string{"a", "b"})
		targets.anchorByBranch["b"] = "deadbeef"

		reanchored := targets.reanchorPastLanded(localBranches(nil), []string{"a"}, "main")

		require.Equal(t, "deadbeef", reanchored["b"].Anchor)
	})
}

// harvestAnchors is the fallback that keeps a branch rebasable when the
// metadata crawl gave up. The crawl abandons the whole walk as soon as one
// ancestor has no metadata — which is the state a merged parent is left in,
// since branch cleanup deletes its remote metadata ref — and the GitHub crawl
// that takes over reads PR bases, which carry no revisions.
func TestHarvestAnchors(t *testing.T) {
	t.Parallel()

	t.Run("fills in anchors the crawl did not record", func(t *testing.T) {
		t.Parallel()
		targets := newTestTargets(map[string]string{"b": "a"}, []string{"a", "b"})

		targets.harvestAnchors(remoteMetadata{
			"b": git.NewMeta().WithParentBranchRevision(strPtr("cafe1234")),
		})

		require.Equal(t, "cafe1234", targets.anchorByBranch["b"])
	})

	t.Run("leaves an anchor the crawl already recorded", func(t *testing.T) {
		t.Parallel()
		targets := newTestTargets(map[string]string{"b": "a"}, []string{"b"})
		targets.anchorByBranch["b"] = "fromcrawl"

		targets.harvestAnchors(remoteMetadata{
			"b": git.NewMeta().WithParentBranchRevision(strPtr("fromcache")),
		})

		require.Equal(t, "fromcrawl", targets.anchorByBranch["b"])
	})

	t.Run("records nothing when the remote metadata has no revision", func(t *testing.T) {
		t.Parallel()
		targets := newTestTargets(map[string]string{"b": "a"}, []string{"b"})

		targets.harvestAnchors(remoteMetadata{
			"b": git.NewMeta(),
		})

		require.Empty(t, targets.anchorByBranch)
	})
}

// The report's two views are what the CLI and the action both key off: only a
// frozen branch with an anchor can be offered, and an unanchored one has to be
// called out instead of silently left behind.
func TestLandedAncestorReportViews(t *testing.T) {
	t.Parallel()

	report := LandedAncestorReport{
		Stale: []StaleBranch{
			{Name: "frozen-anchored", Frozen: true, Anchored: true},
			{Name: "thawed-anchored", Frozen: false, Anchored: true},
			{Name: "frozen-unanchored", Frozen: true, Anchored: false},
		},
	}

	require.Equal(t, []string{"frozen-anchored"}, report.Unfreezable(),
		"an already-thawed branch needs no offer, and an unanchored one must not get it")
	require.Equal(t, []string{"frozen-unanchored"}, report.Unanchored())
}

func strPtr(s string) *string { return &s }
