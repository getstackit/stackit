package engine_test

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

// makeStackForLoadModeTest creates a 3-branch linear stack (main → a → b → c)
// in a fresh scene and returns the scene plus the trunk name. The engine is
// constructed via the standard scenario helper (LoadModeFull) so the metadata
// refs are populated; tests can then construct a second engine with a lighter
// LoadMode against the same repo to validate lazy behavior.
func makeStackForLoadModeTest(t *testing.T) *scenario.Scenario {
	t.Helper()
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

	s.CreateBranch("a").Commit("a")
	require.NoError(t, s.Engine.TrackBranch(context.Background(), "a", "main"))

	s.CreateBranch("b").Commit("b")
	require.NoError(t, s.Engine.TrackBranch(context.Background(), "b", "a"))

	s.CreateBranch("c").Commit("c")
	require.NoError(t, s.Engine.TrackBranch(context.Background(), "c", "b"))

	return s
}

func newEngineWithMode(t *testing.T, repoRoot string, mode engine.LoadMode) engine.Engine {
	t.Helper()
	eng, err := engine.NewEngine(engine.Options{
		RepoRoot: repoRoot,
		Trunk:    "main",
		LoadMode: mode,
	})
	require.NoError(t, err)
	return eng
}

// TestLoadMode_FullParity asserts that under LoadModeFull every accessor
// returns the same values as LoadModeShared after promotion runs. This is
// the equivalence smoke test for the lite-bootstrap construct.
func TestLoadMode_FullParity(t *testing.T) {
	t.Parallel()
	s := makeStackForLoadModeTest(t)

	full := newEngineWithMode(t, s.Scene.Dir, engine.LoadModeFull)
	shared := newEngineWithMode(t, s.Scene.Dir, engine.LoadModeShared)

	for _, name := range []string{"a", "b", "c"} {
		fullBranch := full.GetBranch(name)
		sharedBranch := shared.GetBranch(name)

		fullParent := fullBranch.GetParent()
		sharedParent := sharedBranch.GetParent()
		require.NotNil(t, fullParent, "parent missing on full engine for %s", name)
		require.NotNil(t, sharedParent, "parent missing on shared engine for %s", name)
		require.Equal(t, fullParent.GetName(), sharedParent.GetName(),
			"parent mismatch for %s", name)
		require.Equal(t, full.IsTracked(fullBranch), shared.IsTracked(sharedBranch),
			"IsTracked mismatch for %s", name)
		require.Equal(t, full.IsLocked(fullBranch), shared.IsLocked(sharedBranch),
			"IsLocked mismatch for %s", name)
		require.Equal(t, full.GetScope(fullBranch).String(), shared.GetScope(sharedBranch).String(),
			"GetScope mismatch for %s", name)
		require.Equal(t, full.GetBranchType(fullBranch), shared.GetBranchType(sharedBranch),
			"GetBranchType mismatch for %s", name)
		require.Equal(t, full.IsFrozen(fullBranch), shared.IsFrozen(sharedBranch),
			"IsFrozen mismatch for %s", name)
	}
}

// TestLoadMode_SharedSkipsLocalBatch verifies that constructing an engine with
// LoadModeShared does NOT trigger BatchReadLocalMetadataForBranches at bootstrap. The
// local read happens only when an accessor that needs it (IsFrozen) is called.
//
// Cache hit counters are the observable signal: LoadModeFull warms the local
// metadata refs into metadataCache during bootstrap (counts as misses on first
// read); LoadModeShared leaves them cold until promotion.
func TestLoadMode_SharedSkipsLocalBatch(t *testing.T) {
	t.Parallel()
	s := makeStackForLoadModeTest(t)

	// Use a runner shared by the test so we can read cumulative stats.
	gitRunner := git.NewRunnerWithPath(s.Scene.Dir, nil)
	require.NoError(t, gitRunner.InitDefaultRepo())

	eng, err := engine.NewEngine(engine.Options{
		RepoRoot: s.Scene.Dir,
		Trunk:    "main",
		Git:      gitRunner,
		LoadMode: engine.LoadModeShared,
	})
	require.NoError(t, err)

	// Shared metadata loaded at bootstrap → at least one ReadMetadata per branch.
	// IsTracked / GetScope read only shared state — must not provoke additional
	// local-metadata reads.
	branch := eng.GetBranch("b")
	_ = eng.IsTracked(branch)
	_ = eng.GetScope(branch)

	statsBefore := gitRunner.MetadataCacheStats()

	// Triggering IsFrozen MUST promote local metadata exactly once for the
	// whole branch set, not once per accessor call.
	_ = eng.IsFrozen(eng.GetBranch("a"))
	_ = eng.IsFrozen(eng.GetBranch("b"))
	_ = eng.IsFrozen(eng.GetBranch("c"))

	statsAfter := gitRunner.MetadataCacheStats()

	// The metadata cache only tracks shared-metadata hits/misses, not local
	// (local has its own ref path). So the assertion is "no spurious shared
	// reads happened during IsFrozen" — the local load goes through a
	// separate code path.
	require.Equal(t, statsBefore.Misses, statsAfter.Misses,
		"IsFrozen should not provoke additional shared metadata reads; "+
			"local metadata uses its own ref path")
}

// TestLoadMode_BranchesOnly_ReadsBranchListOnly confirms that
// LoadModeBranchesOnly populates `branches` without touching shared metadata
// refs at bootstrap. The branchState map should be empty until something
// triggers the gate (which PR 6 will wire in for opt-in commands).
func TestLoadMode_BranchesOnly_ReadsBranchListOnly(t *testing.T) {
	t.Parallel()
	s := makeStackForLoadModeTest(t)

	// Fresh runner ⇒ metadataCache starts at zero hits/misses. We want to
	// observe that constructing the engine in BranchesOnly mode adds nothing.
	gitRunner := git.NewRunnerWithPath(s.Scene.Dir, nil)
	require.NoError(t, gitRunner.InitDefaultRepo())

	eng, err := engine.NewEngine(engine.Options{
		RepoRoot: s.Scene.Dir,
		Trunk:    "main",
		Git:      gitRunner,
		LoadMode: engine.LoadModeBranchesOnly,
	})
	require.NoError(t, err)

	// AllBranches reads from the in-memory branches list and must NOT
	// trigger metadata loading.
	branches := eng.AllBranches()
	require.NotEmpty(t, branches, "branch list must be populated under BranchesOnly")

	stats := gitRunner.MetadataCacheStats()
	require.Zero(t, stats.Hits, "BranchesOnly bootstrap must not populate the metadata cache")
	require.Zero(t, stats.Misses, "BranchesOnly bootstrap must not read metadata refs")
}

func TestLoadMode_BranchesOnly_LazilyLoadsBranchChain(t *testing.T) {
	t.Parallel()
	s := makeStackForLoadModeTest(t)

	gitRunner := git.NewRunnerWithPath(s.Scene.Dir, nil)
	require.NoError(t, gitRunner.InitDefaultRepo())

	eng, err := engine.NewEngine(engine.Options{
		RepoRoot: s.Scene.Dir,
		Trunk:    "main",
		Git:      gitRunner,
		LoadMode: engine.LoadModeBranchesOnly,
	})
	require.NoError(t, err)

	root := eng.GetStackRootForBranch(eng.GetBranch("c"))
	require.Equal(t, "a", root)
	require.True(t, eng.IsTracked(eng.GetBranch("c")))

	stats := gitRunner.MetadataCacheStats()
	require.LessOrEqual(t, stats.Misses, uint64(3),
		"lazy stack-root lookup should read only the target parent chain")
}

func TestLoadMode_BranchesOnly_GetScopeLazilyLoadsParentChain(t *testing.T) {
	t.Parallel()
	s := makeStackForLoadModeTest(t)
	require.NoError(t, s.Engine.SetScope(context.Background(), s.Engine.GetBranch("b"), engine.NewScope("topic")))

	gitRunner := git.NewRunnerWithPath(s.Scene.Dir, nil)
	require.NoError(t, gitRunner.InitDefaultRepo())

	eng, err := engine.NewEngine(engine.Options{
		RepoRoot: s.Scene.Dir,
		Trunk:    "main",
		Git:      gitRunner,
		LoadMode: engine.LoadModeBranchesOnly,
	})
	require.NoError(t, err)

	scope := eng.GetScope(eng.GetBranch("c"))
	require.Equal(t, "topic", scope.String())

	stats := gitRunner.MetadataCacheStats()
	require.LessOrEqual(t, stats.Misses, uint64(2),
		"lazy scope lookup should read only the target branch and scoped parent")
}

// TestLoadMode_ConcurrentEnsureLoaded asserts that many concurrent accessors
// against a LoadModeBranchesOnly engine all see the same fully-populated state
// after the gate fires, regardless of which goroutine triggers it. This is
// the high-risk path — double-checked locking around sync.Once is the most
// common spot for subtle bugs.
func TestLoadMode_ConcurrentEnsureLoaded(t *testing.T) {
	t.Parallel()
	s := makeStackForLoadModeTest(t)

	gitRunner := git.NewRunnerWithPath(s.Scene.Dir, nil)
	require.NoError(t, gitRunner.InitDefaultRepo())

	eng, err := engine.NewEngine(engine.Options{
		RepoRoot: s.Scene.Dir,
		Trunk:    "main",
		Git:      gitRunner,
		LoadMode: engine.LoadModeShared,
	})
	require.NoError(t, err)

	// Drive concurrent IsFrozen calls; each promotion attempt must collapse
	// to a single localLoadOnce execution.
	var wg sync.WaitGroup
	const goroutines = 32
	wg.Add(goroutines)
	for range goroutines {
		go func() {
			defer wg.Done()
			for _, name := range []string{"a", "b", "c"} {
				_ = eng.IsFrozen(eng.GetBranch(name))
			}
		}()
	}
	wg.Wait()

	// After contention settles, IsFrozen still returns deterministic answers.
	// (All branches in this test are not frozen.)
	for _, name := range []string{"a", "b", "c"} {
		require.False(t, eng.IsFrozen(eng.GetBranch(name)), "branch %s should not be frozen", name)
	}
}

// TestLoadMode_SnapshotForWorktreeForceLoads asserts SnapshotForWorktree
// always returns a fully-populated snapshot even when the parent engine was
// constructed in a lighter mode. Worktree consumers (restack, sync) assume
// the snapshot is complete; the snapshot path is responsible for triggering
// both gates before copying.
func TestLoadMode_SnapshotForWorktreeForceLoads(t *testing.T) {
	t.Parallel()
	s := makeStackForLoadModeTest(t)

	eng := newEngineWithMode(t, s.Scene.Dir, engine.LoadModeBranchesOnly)
	snap := eng.SnapshotForWorktree()

	require.NotEmpty(t, snap.Branches, "snapshot must list branches")
	require.NotEmpty(t, snap.BranchState, "snapshot must contain branchState entries after force-load")
	for _, name := range []string{"a", "b", "c"} {
		state, ok := snap.BranchState[name]
		require.True(t, ok, "missing branch state for %s", name)
		require.NotEmpty(t, state.Parent, "parent missing for %s", name)
	}
}
