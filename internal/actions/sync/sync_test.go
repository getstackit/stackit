package sync

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

func TestSyncAction(t *testing.T) {
	t.Parallel()
	t.Run("syncs when trunk is up to date", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewRemoteScenario(t)

		err := Action(s.Context, Options{
			All:     false,
			Force:   false,
			Restack: false,
		}, nil)
		require.NoError(t, err)
	})

	t.Run("fails when required trunk fetch fails", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

		err := Action(s.Context, Options{}, nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to fetch trunk from")
	})

	// Note: uncommitted changes check moved to CLI layer (sync.go) to run
	// before TUI initialization. Tested in internal/cli/stack/sync_test.go.

	t.Run("syncs with restack flag", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewRemoteScenario(t).
			WithStack(map[string]string{
				"branch1": "main",
			})

		err := Action(s.Context, Options{
			All:     false,
			Force:   false,
			Restack: true,
		}, nil)
		// Should succeed (even if no restacking needed)
		require.NoError(t, err)
	})

	t.Run("restacks branches in topological order (parents before children)", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewRemoteScenario(t).
			WithStack(map[string]string{
				"branch1": "main",
				"branch2": "branch1",
				"branch3": "branch2",
			})

		err := Action(s.Context, Options{
			All:     false,
			Force:   false,
			Restack: true,
		}, nil)
		// Should succeed - branches should be restacked in correct order
		require.NoError(t, err)
	})

	t.Run("restacks branching stacks in topological order", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewRemoteScenario(t).
			WithStack(map[string]string{
				"stackA":        "main",
				"stackA-child1": "stackA",
				"stackA-child2": "stackA",
				"stackB":        "main",
				"stackB-child1": "stackB",
			})

		err := Action(s.Context, Options{
			All:     false,
			Force:   false,
			Restack: true,
		}, nil)
		// Should succeed - branches should be restacked with parents before children
		require.NoError(t, err)

		// Verify all branches still exist and are properly tracked
		s.ExpectStackStructure(map[string]string{
			"stackA":        "main",
			"stackA-child1": "stackA",
			"stackA-child2": "stackA",
			"stackB":        "main",
			"stackB-child1": "stackB",
		})
	})

	t.Run("restacks multiple deep subtrees correctly", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewRemoteScenario(t).
			WithStack(map[string]string{
				"P":   "main",
				"C1":  "P",
				"GC1": "C1",
				"C2":  "P",
				"GC2": "C2",
			})

		// Modify P to trigger restacking of all descendants
		s.Checkout("P").
			Commit("P updated")

		// Refresh engine
		err := s.Engine.Rebuild("main")
		require.NoError(t, err)

		err = Action(s.Context, Options{
			All:     true,
			Restack: true,
		}, nil)
		require.NoError(t, err)

		// Verify all branches are fixed
		s.ExpectBranchFixed("C1").
			ExpectBranchFixed("GC1").
			ExpectBranchFixed("C2").
			ExpectBranchFixed("GC2")
	})

	t.Run("partial success in branching restack (one child succeeds, one fails)", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewRemoteScenario(t)

		// Create: main -> P -> [0-Success, 1-Failure]
		// We use these names because siblings are sorted ascending by name by default,
		// and we want the successful one to be processed first.
		s.CreateBranch("P").
			Commit("P change").
			TrackBranch("P", "main")

		// 0-Success will restack successfully
		s.Checkout("P").
			CreateBranch("0-Success").
			Commit("Success change").
			TrackBranch("0-Success", "P")

		// 1-Failure will have a conflict
		s.Checkout("P").
			CreateBranch("1-Failure")
		err := s.Scene.Repo.CreateChangeAndCommit("initial content", "conflict")
		require.NoError(t, err)
		s.TrackBranch("1-Failure", "P")

		// Modify P with a change that conflicts with 1-Failure but not 0-Success
		s.Checkout("P")
		err = s.Scene.Repo.CreateChangeAndCommit("conflicting content", "conflict")
		require.NoError(t, err)

		// Refresh engine
		err = s.Engine.Rebuild("main")
		require.NoError(t, err)

		s.Checkout("P")

		handler := &NullHandler{}
		err = Action(s.Context, Options{
			All:     true,
			Restack: true,
		}, handler)

		// Should NOT error - conflicts are detected via validation and skipped
		require.NoError(t, err)

		// 0-Success should have been restacked successfully
		s.ExpectBranchFixed("0-Success")
		// 1-Failure should NOT be fixed (conflict detected via validation)
		s.ExpectBranchNotFixed("1-Failure")
	})

	t.Run("sync mode aborts unexpected runtime restack conflict and restores branch", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewRemoteScenario(t).
			WithStack(map[string]string{
				"branch1": "main",
			})

		// Move trunk so branch1 needs restacking.
		s.Checkout("main").
			CommitChange("main-update", "advance trunk")
		s.Checkout("branch1")

		// Recreate the engine with a wrapper that injects a runtime rebase conflict.
		wrappedGit := &runtimeConflictRunner{
			Runner:         s.Engine.Git(),
			conflictBranch: "branch1",
		}

		eng, err := engine.NewEngine(engine.Options{
			RepoRoot: s.Scene.Dir,
			Trunk:    "main",
			Git:      wrappedGit,
		})
		require.NoError(t, err)
		s.Engine = eng
		s.Context.Engine = eng

		err = Action(s.Context, Options{
			Restack: true,
		}, nil)
		require.NoError(t, err)
		require.True(t, wrappedGit.abortCalled, "sync should abort an unexpected in-progress rebase")
		require.False(t, wrappedGit.IsRebaseInProgress(s.Context), "sync should finish with no rebase in progress")

		current := s.Engine.CurrentBranch()
		require.NotNil(t, current, "sync should restore original branch checkout")
		require.Equal(t, "branch1", current.GetName())
	})
}

type runtimeConflictRunner struct {
	git.Runner
	conflictBranch   string
	rebaseInProgress bool
	abortCalled      bool
	injected         bool
}

func (r *runtimeConflictRunner) Rebase(ctx context.Context, branchName, upstream, oldUpstream string) (git.RebaseOutcome, error) {
	if branchName == r.conflictBranch && !r.injected {
		r.injected = true
		r.rebaseInProgress = true
		_ = r.CheckoutDetached(ctx, branchName)
		return git.RebaseOutcome{Result: git.RebaseConflict}, nil
	}
	return r.Runner.Rebase(ctx, branchName, upstream, oldUpstream)
}

func (r *runtimeConflictRunner) UpdateRefsBatch(ctx context.Context, updates []git.RefUpdate) error {
	if r.injectRuntimeConflict() {
		return nil
	}
	return r.Runner.UpdateRefsBatch(ctx, updates)
}

func (r *runtimeConflictRunner) UpdateRefsBatchWithLog(ctx context.Context, updates []git.RefUpdate, reflogMessage string) error {
	if r.injectRuntimeConflict() {
		return nil
	}
	return r.Runner.UpdateRefsBatchWithLog(ctx, updates, reflogMessage)
}

func (r *runtimeConflictRunner) injectRuntimeConflict() bool {
	if !r.injected {
		r.injected = true
		r.rebaseInProgress = true
		return true
	}
	return false
}

func (r *runtimeConflictRunner) CheckoutBranch(ctx context.Context, branchName string) error {
	if r.rebaseInProgress {
		return fmt.Errorf("cannot checkout %s while rebase is in progress", branchName)
	}
	return r.Runner.CheckoutBranch(ctx, branchName)
}

func (r *runtimeConflictRunner) IsRebaseInProgress(ctx context.Context) bool {
	if r.rebaseInProgress {
		return true
	}
	return r.Runner.IsRebaseInProgress(ctx)
}

func (r *runtimeConflictRunner) RebaseAbort(ctx context.Context) error {
	if r.rebaseInProgress {
		r.rebaseInProgress = false
		r.abortCalled = true
		return nil
	}
	return r.Runner.RebaseAbort(ctx)
}
