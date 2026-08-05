package engine

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestBranchHeldBackCyclicMetadata covers the ancestry walk that decides whether
// a branch sits behind a held one.
//
// The walk followed Parent links with no visited set and no step limit, so a
// parent loop in metadata — corrupt, or concurrently rewritten by another
// stackit process mid-pass — spun forever and hung the entire restack.
func TestBranchHeldBackCyclicMetadata(t *testing.T) {
	t.Parallel()

	newEngine := func(t *testing.T, states map[string]string) *engineImpl {
		t.Helper()
		dir, g := newOptimisticLockTestRepo(t)
		eng, err := NewEngine(Options{RepoRoot: dir, Trunk: "main", Git: g})
		require.NoError(t, err)
		e, ok := eng.(*engineImpl)
		require.True(t, ok)

		e.mu.Lock()
		for name, parent := range states {
			e.state.branchState.Set(name, &BranchState{Parent: parent})
		}
		e.mu.Unlock()
		return e
	}

	// The walk is capped by branch count, so a hang shows up as a test timeout
	// rather than a wrong answer. Run it with a deadline to keep the failure
	// legible instead of letting the whole package time out.
	within := func(t *testing.T, fn func() bool) bool {
		t.Helper()
		done := make(chan bool, 1)
		go func() { done <- fn() }()
		select {
		case got := <-done:
			return got
		case <-time.After(20 * time.Second):
			t.Fatal("branchHeldBack did not terminate: the ancestry walk is unbounded")
			return false
		}
	}

	t.Run("cycle terminates and holds the branch", func(t *testing.T) {
		t.Parallel()
		e := newEngine(t, map[string]string{
			"a": "b",
			"b": "c",
			"c": "a",
		})
		snap := &restackSnapshot{heldBranches: make(BranchNameSet)}

		// Nothing is held, but the parent chain never reaches trunk. Metadata
		// that cannot say what a branch is stacked on is not safe to build on.
		held := within(t, func() bool { return e.branchHeldBack(NewBranch("a", nil), snap) })
		require.True(t, held)
	})

	t.Run("self-parent terminates", func(t *testing.T) {
		t.Parallel()
		e := newEngine(t, map[string]string{"a": "a"})
		snap := &restackSnapshot{heldBranches: make(BranchNameSet)}

		held := within(t, func() bool { return e.branchHeldBack(NewBranch("a", nil), snap) })
		require.True(t, held)
	})

	t.Run("acyclic chain still resolves normally", func(t *testing.T) {
		t.Parallel()
		e := newEngine(t, map[string]string{
			"child":  "parent",
			"parent": "main",
		})

		snap := &restackSnapshot{heldBranches: make(BranchNameSet)}
		require.False(t, e.branchHeldBack(NewBranch("child", nil), snap))

		snap.heldBranches["parent"] = true
		require.True(t, e.branchHeldBack(NewBranch("child", nil), snap))
	})
}
